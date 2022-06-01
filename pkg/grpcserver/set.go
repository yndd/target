/*
Copyright 2021 NDDO.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package grpcserver

import (
	"context"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-yang/pkg/yparser"
	"github.com/yndd/ndd-runtime/pkg/targetchannel"
	"github.com/yndd/target/pkg/validator"
	"github.com/yndd/target/pkg/cachename"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/yndd/ndd-runtime/pkg/meta"
)

func (s *GrpcServerImpl) Set(ctx context.Context, req *gnmi.SetRequest) (*gnmi.SetResponse, error) {

	ok := s.unaryRPCsem.TryAcquire(1)
	if !ok {
		return nil, status.Errorf(codes.ResourceExhausted, errMaxNbrOfUnaryRPCReached)
	}
	defer s.unaryRPCsem.Release(1)

	prefix := req.GetPrefix()
	cacheNsTargetName := meta.NamespacedName(prefix.GetTarget()).GetPrefixNamespacedName(prefix.GetOrigin())

	numUpdates := len(req.GetUpdate())
	numReplaces := len(req.GetReplace())
	numDeletes := len(req.GetDelete())
	if numUpdates+numReplaces+numDeletes == 0 {
		// for origin == target cache updates we can have an empty path
		if prefix.GetOrigin() != cachename.TargetCachePrefix {
			return nil, status.Errorf(codes.InvalidArgument, errMissingPathsInGNMISet)
		}
	}

	log := s.log.WithValues(
		"origin", prefix.GetOrigin(),
		"target", prefix.GetTarget(),
		"numUpdates", numUpdates,
		"numReplaces", numReplaces,
		"numDeletes", numDeletes,
	)

	ce, err := s.cache.GetEntry(cacheNsTargetName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, errTargetNotFoundInCache)
	}

	if numReplaces > 0 {
		log.Debug("Set Replace", "Path", yparser.GnmiPath2XPath(req.GetReplace()[0].GetPath(), true))

		if prefix.GetOrigin() == cachename.TargetCachePrefix {
			// for target configs we have to create the entry
			v, err := yparser.GetValue(req.GetReplace()[0].GetVal())
			if err != nil {
				return nil, status.Errorf(codes.Internal, err.Error())
			}
			newGoStruct, err := validator.ValidateCreate(ce, v)
			if err != nil {
				return nil, status.Errorf(codes.Internal, err.Error())
			}
			ce.SetRunningConfig(newGoStruct)
		} else {
			if err := validator.ValidateUpdate(ce, req.GetReplace(), true, false, validator.Origin_GnmiServer); err != nil {
				return nil, status.Errorf(codes.Internal, err.Error())
			}
		}

	}

	if numUpdates > 0 {
		log.Debug("Set Update", "target", prefix.Target, "Path", yparser.GnmiPath2XPath(req.GetUpdate()[0].GetPath(), true))
		// check if the update is a transaction or not -> determines if the individual reconciler has to run
		return nil, status.Errorf(codes.Unimplemented, "not implemented")
	}

	if numDeletes > 0 {
		log.Debug("Set Delete", "target", prefix.Target, "Path", yparser.GnmiPath2XPath(req.GetDelete()[0], true))
		// check if the update is a transaction or not -> determines if the individual reconciler has to run
		return nil, status.Errorf(codes.Unimplemented, "not implemented")
	}

	// the gnmi proxy cache holds 3 origins: target, system and config.
	// the set will only update origin = target or system, not config
	// for target updates we need to inform the the targetDriver through the channel
	// for system cache updates we need to set a flag such that the target reconciler picks it up
	if prefix.GetOrigin() == cachename.TargetCachePrefix {
		// we only perform replace or delete on the target object -> replace = start, delete = stop
		targetOperation := targetchannel.Start
		// in the target case the delete is represented with 0 paths
		if numUpdates+numReplaces+numDeletes == 0 {
			targetOperation = targetchannel.Stop
		}
		s.targetChannel <- targetchannel.TargetMsg{
			Target:    prefix.GetTarget(),
			Operation: targetOperation,
		}
	} else {
		if err := ce.SetSystemCacheStatus(true); err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
	}

	return &gnmi.SetResponse{
		Response: []*gnmi.UpdateResult{
			{
				Timestamp: time.Now().UnixNano(),
			},
		}}, nil
}
