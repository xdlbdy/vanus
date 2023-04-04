// Copyright 2022 Linkall Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validation

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	cesqlparser "github.com/cloudevents/sdk-go/sql/v2/parser"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"

	"github.com/vanus-labs/vanus/internal/primitive/vanus"
	"github.com/vanus-labs/vanus/pkg/errors"
	ctrlpb "github.com/vanus-labs/vanus/proto/pkg/controller"
	metapb "github.com/vanus-labs/vanus/proto/pkg/meta"

	"github.com/vanus-labs/vanus/internal/primitive"
	"github.com/vanus-labs/vanus/internal/primitive/cel"
	"github.com/vanus-labs/vanus/internal/primitive/transform/arg"
	"github.com/vanus-labs/vanus/internal/primitive/transform/runtime"
)

func ValidateSubscriptionRequest(ctx context.Context, request *ctrlpb.SubscriptionRequest) error {
	if request.NamespaceId == vanus.EmptyID().Uint64() {
		return errors.ErrInvalidRequest.WithMessage("namespace is empty")
	}
	if request.EventbusId == vanus.EmptyID().Uint64() {
		return errors.ErrInvalidRequest.WithMessage("eventbus is empty")
	}
	if err := ValidateFilterList(ctx, request.Filters); err != nil {
		return errors.ErrInvalidRequest.WithMessage("filters is invalid").Wrap(err)
	}
	if err := validateProtocol(ctx, request.Protocol); err != nil {
		return err
	}
	if err := ValidateSinkAndProtocol(ctx, request.Sink, request.Protocol, request.SinkCredential); err != nil {
		return err
	}
	if err := validateSinkCredential(ctx, request.Sink, request.SinkCredential); err != nil {
		return err
	}
	if request.Name == "" {
		return errors.ErrInvalidRequest.WithMessage("name is empty")
	}
	if err := validateSubscriptionConfig(ctx, request.Config); err != nil {
		return err
	}
	if err := validateTransformer(ctx, request.Transformer); err != nil {
		return err
	}
	return nil
}

func validateProtocol(ctx context.Context, protocol metapb.Protocol) error {
	switch protocol {
	case metapb.Protocol_HTTP:
	case metapb.Protocol_AWS_LAMBDA:
	case metapb.Protocol_GCLOUD_FUNCTIONS:
	case metapb.Protocol_GRPC:

	default:
		return errors.ErrInvalidRequest.WithMessage("protocol is invalid")
	}
	return nil
}

func ValidateSinkAndProtocol(ctx context.Context,
	sink string,
	protocol metapb.Protocol,
	credential *metapb.SinkCredential,
) error {
	if sink == "" {
		return errors.ErrInvalidRequest.WithMessage("sink is empty")
	}
	switch protocol {
	case metapb.Protocol_AWS_LAMBDA:
		if _, err := arn.Parse(sink); err != nil {
			return errors.ErrInvalidRequest.
				WithMessage("protocol is aws lambda, sink is arn, arn parse error").Wrap(err)
		}
		if credential.GetCredentialType() != metapb.SinkCredential_AWS {
			return errors.ErrInvalidRequest.
				WithMessage("protocol is aws lambda, sink credential can not be nil and credential type is aws")
		}
	case metapb.Protocol_GCLOUD_FUNCTIONS:
		if credential.GetCredentialType() != metapb.SinkCredential_GCLOUD {
			return errors.ErrInvalidRequest.
				WithMessage("protocol is gcloud functions, sink credential can not be nil and credential type is gcloud")
		}
	case metapb.Protocol_HTTP:
		if _, err := url.Parse(sink); err != nil {
			return errors.ErrInvalidRequest.
				WithMessage("protocol is http, sink is url,url parse error").Wrap(err)
		}
	case metapb.Protocol_GRPC:
	}
	return nil
}

func validateSinkCredential(ctx context.Context, sink string, credential *metapb.SinkCredential) error {
	if credential == nil {
		return nil
	}
	switch credential.CredentialType {
	case metapb.SinkCredential_None:
	case metapb.SinkCredential_PLAIN:
		if credential.GetPlain().GetIdentifier() == "" ||
			credential.GetPlain().GetSecret() == "" {
			return errors.ErrInvalidRequest.WithMessage(
				"sink credential type is plain,Identifier and Secret can not empty")
		}
	case metapb.SinkCredential_AWS:
		if credential.GetAws().GetAccessKeyId() == "" ||
			credential.GetAws().GetSecretAccessKey() == "" {
			return errors.ErrInvalidRequest.
				WithMessage("sink credential type is aws,accessKeyId and SecretAccessKey can not empty")
		}
	case metapb.SinkCredential_GCLOUD:
		credentialJSON := credential.GetGcloud().GetCredentialsJson()
		if credentialJSON == "" {
			return errors.ErrInvalidRequest.
				WithMessage("sink credential type is gcloud,credential json can not empty")
		}
		_, err := idtoken.NewTokenSource(ctx, sink,
			option.WithCredentialsJSON([]byte(credentialJSON)))
		if err != nil {
			return errors.ErrInvalidRequest.
				WithMessage("gcloud credential json invalid").Wrap(err)
		}
	default:
		return errors.ErrInvalidRequest.WithMessage("sink credential type is invalid")
	}
	return nil
}

func validateSubscriptionConfig(ctx context.Context, cfg *metapb.SubscriptionConfig) error {
	if cfg == nil {
		return nil
	}
	switch cfg.OffsetType {
	case metapb.SubscriptionConfig_LATEST, metapb.SubscriptionConfig_EARLIEST:
	case metapb.SubscriptionConfig_TIMESTAMP:
		if cfg.OffsetTimestamp == nil {
			return errors.ErrInvalidRequest.WithMessage(
				"offset type is timestamp, offset timestamp can not be nil")
		}
	default:
		return errors.ErrInvalidRequest.WithMessage("offset type is invalid")
	}
	if cfg.GetMaxRetryAttempts() > primitive.MaxRetryAttempts {
		return errors.ErrInvalidRequest.WithMessage(
			fmt.Sprintf("could not set max retry attempts greater than %d", primitive.MaxRetryAttempts))
	}
	return nil
}

func validateTransformer(ctx context.Context, transformer *metapb.Transformer) error {
	if transformer == nil {
		return nil
	}
	if len(transformer.Define) > 0 {
		for key, value := range transformer.Define {
			_, err := arg.NewArg(value)
			if err != nil {
				return errors.ErrInvalidRequest.WithMessage(
					fmt.Sprintf("transformer define %s:%s is invalid:[%s]", key, value, err.Error()))
			}
		}
	}
	if len(transformer.Pipeline) > 0 {
		for n, a := range transformer.Pipeline {
			commands := make([]interface{}, len(a.Command))
			for i, command := range a.Command {
				commands[i] = command.AsInterface()
			}
			if _, err := runtime.NewAction(commands); err != nil {
				return errors.ErrInvalidRequest.WithMessage(
					fmt.Sprintf("transformer pipeline %dst command %s is invalid:[%s]", n+1, commands[0], err.Error()))
			}
		}
	}
	return nil
}

func ValidateFilterList(ctx context.Context, filters []*metapb.Filter) error {
	if len(filters) == 0 {
		return nil
	}
	for _, f := range filters {
		if f == nil {
			continue
		}
		if err := ValidateFilter(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func ValidateFilter(ctx context.Context, f *metapb.Filter) error {
	if hasMultipleDialects(f) {
		return errors.ErrFilterMultiple.WithMessage("filters can have only one dialect")
	}
	if err := validateAttributeMap("exact", f.Exact); err != nil {
		return err
	}
	if err := validateAttributeMap("prefix", f.Prefix); err != nil {
		return err
	}
	if err := validateAttributeMap("suffix", f.Suffix); err != nil {
		return err
	}
	if err := validateAttributeMap("contains", f.Contains); err != nil {
		return err
	}
	if f.Sql != "" {
		if err := validateCeSQL(ctx, f.Sql); err != nil {
			return err
		}
	}
	if f.Cel != "" {
		if err := validateCel(ctx, f.Cel); err != nil {
			return err
		}
	}
	if f.Not != nil {
		if err := ValidateFilter(ctx, f.Not); err != nil {
			return errors.ErrInvalidRequest.WithMessage("not filter dialect invalid").Wrap(err)
		}
	}
	if err := ValidateFilterList(ctx, f.All); err != nil {
		return errors.ErrInvalidRequest.WithMessage("all filter dialect invalid").Wrap(err)
	}
	if err := ValidateFilterList(ctx, f.Any); err != nil {
		return errors.ErrInvalidRequest.WithMessage("any filter dialect invalid").Wrap(err)
	}
	return nil
}

func validateCel(ctx context.Context, expression string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.ErrCelExpression.WithMessage(expression)
		}
	}()
	if _, err = cel.Parse(expression); err != nil {
		return errors.ErrCelExpression.WithMessage(expression).Wrap(err)
	}
	return err
}

func validateCeSQL(ctx context.Context, expression string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.ErrCeSQLExpression.WithMessage(expression)
		}
	}()
	if _, err = cesqlparser.Parse(expression); err != nil {
		return errors.ErrCeSQLExpression.WithMessage(expression).Wrap(err)
	}
	return err
}

func validateAttributeMap(attributeName string, attribute map[string]string) error {
	if len(attribute) == 0 {
		return nil
	}
	for k, v := range attribute {
		if k == "" {
			return errors.ErrFilterAttributeIsEmpty.WithMessage(
				attributeName + " filter dialect attribute name must not empty")
		}
		if v == "" {
			return errors.ErrFilterAttributeIsEmpty.WithMessage(
				attributeName + " filter dialect attribute value must not empty")
		}
	}
	return nil
}

func hasMultipleDialects(f *metapb.Filter) bool {
	dialectFound := false
	if len(f.Exact) > 0 {
		dialectFound = true
	}
	if len(f.Prefix) > 0 {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if len(f.Suffix) > 0 {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if len(f.Contains) > 0 {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if len(f.All) > 0 {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if len(f.Any) > 0 {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if f.Not != nil {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if f.Sql != "" {
		if dialectFound {
			return true
		}
		dialectFound = true
	}
	if f.Cel != "" && dialectFound {
		return true
	}
	return false
}
