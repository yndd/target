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

package configgnmihandler

import (
	"context"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// errors
	errTargetNotFoundInCache = "could not find target in cache"
)

func (s *subServer) Set(ctx context.Context, p *gnmi.Path, upd *gnmi.Update) (*gnmi.SetResponse, error) {
	cacheNsTargetName := meta.NamespacedName(p.GetTarget()).GetPrefixNamespacedName(p.GetOrigin())

	ce, err := s.cache.GetEntry(cacheNsTargetName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, errTargetNotFoundInCache)
	}

	if err := ce.SetSystemCacheStatus(true); err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &gnmi.SetResponse{
		Response: []*gnmi.UpdateResult{
			{
				Timestamp: time.Now().UnixNano(),
			},
		}}, nil
}

func (s *subServer) Delete(ctx context.Context, p *gnmi.Path, del *gnmi.Path) (*gnmi.SetResponse, error) {
	cacheNsTargetName := meta.NamespacedName(p.GetTarget()).GetPrefixNamespacedName(p.GetOrigin())

	ce, err := s.cache.GetEntry(cacheNsTargetName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, errTargetNotFoundInCache)
	}

	if err := ce.SetSystemCacheStatus(true); err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &gnmi.SetResponse{
		Response: []*gnmi.UpdateResult{
			{
				Timestamp: time.Now().UnixNano(),
			},
		}}, nil
}
