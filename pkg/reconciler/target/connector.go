/*
Copyright 2021 NDD.

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

package target

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/karimra/gnmic/target"
	"github.com/karimra/gnmic/types"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ygot"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/utils"
	targetv1 "github.com/yndd/ndd-target-runtime/apis/dvr/v1"
	"github.com/yndd/ndd-target-runtime/internal/model"
	"github.com/yndd/ndd-target-runtime/pkg/ygotnddtarget"
	"github.com/yndd/ndd-yang/pkg/yparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// target
	TargetCachePrefix = "target"
	// errors
	errJSONMarshal     = "cannot marshal JSON object"
	errObserveResource = "cannot observe Target"
	errCreateResource  = "cannot create Target"
	errDeleteResource  = "cannot delete Target"
)

// An GnmiConnecter produces a new GnmiClient given the supplied address
type GnmiConnecter interface {
	// Connect to the supplied address and produce an ExternalClient.
	Connect(ctx context.Context, address string) (ExternalClient, error)
}

// An ExternalClient manages the lifecycle of a target cr resource.
type ExternalClient interface {
	// Observe the target status
	Observe(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) (Observation, error)

	// Create a target
	Create(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) error

	// Delete the target
	Delete(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) error

	// Close the connection to the target
	Close()
}

// An Observation is the result of an observation of a target
type Observation struct {
	// Exists identifies if a target was already create
	Exists bool
	// IsUpToDate identifies if the running target config is up to date with the spec target config
	IsUpToDate bool
	// Discovered indicates if the target is discovered
	Discovered bool
	// DiscoveryInfo identifies the information discovered from the target
	DiscoveryInfo *targetv1.DiscoveryInfo
}

type connector struct {
	log         logging.Logger
	m           *model.Model
	fm          *model.Model
	newClientFn func(c *types.TargetConfig) *target.Target
}

func (c *connector) Connect(ctx context.Context, address string) (ExternalClient, error) {
	cfg := &types.TargetConfig{
		Name:       "targetReconcileController",
		Address:    address,
		Username:   utils.StringPtr("admin"),
		Password:   utils.StringPtr("admin"),
		Timeout:    10 * time.Second,
		SkipVerify: utils.BoolPtr(true),
		Insecure:   utils.BoolPtr(true),
		TLSCA:      utils.StringPtr(""), //TODO TLS
		TLSCert:    utils.StringPtr(""), //TODO TLS
		TLSKey:     utils.StringPtr(""),
		Gzip:       utils.BoolPtr(false),
	}

	cl := c.newClientFn(cfg)
	//cl := target.NewTarget(cfg)
	if err := cl.CreateGNMIClient(ctx); err != nil {
		return nil, errors.Wrap(err, "error creating new client")
	}
	return &external{client: cl, log: c.log, m: c.m, fm: c.fm}, nil
}

type external struct {
	client *target.Target
	log    logging.Logger
	m      *model.Model
	fm     *model.Model
}

func (e *external) Close() {
	e.client.Close()
}

func (e *external) Observe(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) (Observation, error) {
	log := e.log.WithValues("Target", tspec.Name)
	log.Debug("Observing ...")

	crTarget := strings.Join([]string{TargetCachePrefix, namespace, *tspec.Name}, ".")

	req := &gnmi.GetRequest{
		Prefix:   &gnmi.Path{Target: crTarget},
		Path:     []*gnmi.Path{{}},
		Encoding: gnmi.Encoding_JSON,
	}

	// gnmi get response
	resp, err := e.client.Get(ctx, req)
	if err != nil {
		log.Debug("Observing ...", "error", err)
		if er, ok := status.FromError(err); ok {
			switch er.Code() {
			case codes.Unavailable:
				// we use this to signal not ready
				return Observation{}, nil
			case codes.NotFound:
				return Observation{}, nil
			}
		}
	}

	var cacheTarget interface{}
	if len(resp.GetNotification()) == 0 {
		return Observation{}, nil
	}
	if len(resp.GetNotification()) != 0 && len(resp.GetNotification()[0].GetUpdate()) != 0 {
		// get value from gnmi get response
		cacheTarget, err = yparser.GetValue(resp.GetNotification()[0].GetUpdate()[0].Val)
		if err != nil {
			return Observation{}, errors.Wrap(err, errJSONMarshal)
		}

		switch cacheTarget.(type) {
		case nil:
			// resource has no data
			return Observation{}, nil
		}
	}

	cacheTargetData, err := json.Marshal(cacheTarget)
	if err != nil {
		return Observation{}, err
	}
	log.Debug("Observing ...", "cacheStateData", string(cacheTargetData))

	// validate the target cache as a validtedGoStruct
	validatedGoStruct, err := e.fm.NewConfigStruct(cacheTargetData, true)
	if err != nil {
		return Observation{}, err
	}
	// type casting
	cacheNddTargetDevice, ok := validatedGoStruct.(*ygotnddtarget.Device)
	if !ok {
		return Observation{}, errors.New("wrong ndd target object")
	}

	log.Debug("Observing ...", "cacheNddTargetDevice", cacheNddTargetDevice)

	// check if the entry exists
	cacheTargetEntry, ok := cacheNddTargetDevice.TargetEntry[*tspec.Name]
	if !ok {
		return Observation{}, nil
	}

	log.Debug("Observing ...", "cacheStateEntry", cacheTargetEntry)

	discoveryInfo := getDiscoveryInfo(cacheTargetEntry)
	discovered := true
	if discoveryInfo == nil {
		discovered = false
	}

	log.Debug("Observing ...", "discoveryInfo", discoveryInfo)

	// check if the cacheData is aligned with the crSpecData
	// we remove the state before comparing the spec
	cacheTargetEntry.State = nil
	deletes, updates, err := e.diff(tspec, cacheTargetEntry)
	if err != nil {
		return Observation{}, err
	}

	return Observation{
		Exists:        true,
		IsUpToDate:    len(deletes) == 0 && len(updates) == 0,
		Discovered:    discovered,
		DiscoveryInfo: discoveryInfo,
	}, nil

}

func (e *external) Create(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) error {
	log := e.log.WithValues("Target", tspec.Name)
	log.Debug("Creating ...")

	updates, err := e.getUpate(tspec)
	if err != nil {
		return errors.Wrap(err, errCreateResource)
	}

	crTarget := strings.Join([]string{TargetCachePrefix, namespace, *tspec.Name}, ".")

	req := &gnmi.SetRequest{
		Prefix:  &gnmi.Path{Target: crTarget},
		Replace: updates,
	}

	_, err = e.client.Set(ctx, req)
	if err != nil {
		return errors.Wrap(err, errCreateResource)
	}

	return nil
}

func (e *external) Delete(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) error {
	log := e.log.WithValues("Target", tspec.Name)
	log.Debug("Deleting ...")

	crTarget := strings.Join([]string{TargetCachePrefix, namespace, *tspec.Name}, ".")

	req := &gnmi.SetRequest{
		Prefix: &gnmi.Path{Target: crTarget},
		Delete: []*gnmi.Path{
			{
				Elem: []*gnmi.PathElem{
					{Name: "target-entry", Key: map[string]string{"name": *tspec.Name}},
				},
			},
		},
	}

	_, err := e.client.Set(ctx, req)
	if err != nil {
		return errors.Wrap(err, errDeleteResource)
	}

	return nil
}

// getUpate returns an update to the cache
func (e *external) getUpate(tspec *ygotnddtarget.NddTarget_TargetEntry) ([]*gnmi.Update, error) {
	e.log.Debug("getUpate")

	targetEntryJson, err := ygot.EmitJSON(tspec, &ygot.EmitJSONConfig{
		Format: ygot.RFC7951,
	})
	if err != nil {
		return nil, err
	}

	//return update
	return []*gnmi.Update{
		{
			Path: &gnmi.Path{
				Elem: []*gnmi.PathElem{
					{Name: "state-entry", Key: map[string]string{"name": *tspec.Name}},
				},
			},
			Val: &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{JsonVal: []byte(targetEntryJson)}},
		},
	}, nil
}

func (e *external) diff(specTargetEntryConfig, cacheTargetEntryConfig interface{}) ([]*gnmi.Path, []*gnmi.Update, error) {
	specConfig, ok := specTargetEntryConfig.(ygot.ValidatedGoStruct)
	if !ok {
		return nil, nil, errors.New("invalid Object")
	}

	e.log.Debug("observe diff", "specConfig", specConfig)

	cacheConfig, ok := cacheTargetEntryConfig.(ygot.ValidatedGoStruct)
	if !ok {
		return nil, nil, errors.New("invalid Object")
	}
	e.log.Debug("observe diff", "cacheConfig", cacheConfig)

	// create a diff of the actual compared to the to-become-new config
	actualVsSpecDiff, err := ygot.Diff(specConfig, cacheConfig, &ygot.DiffPathOpt{MapToSinglePath: true})
	if err != nil {
		return nil, nil, err
	}

	deletes, updates := validateNotification(actualVsSpecDiff)
	return deletes, updates, nil
}

func validateNotification(n *gnmi.Notification) ([]*gnmi.Path, []*gnmi.Update) {
	updates := make([]*gnmi.Update, 0)
	for _, u := range n.GetUpdate() {
		fmt.Printf("validateNotification diff update old path: %s, value: %v\n", yparser.GnmiPath2XPath(u.GetPath(), true), u.GetVal())
		// workaround since the diff can return double pathElem
		var changed bool
		changed, u.Path = validatePath(u.GetPath())
		if changed {
			u.Val = &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{JsonVal: []byte("{}")}}
		}
		fmt.Printf("validateNotification diff update new path: %s, value: %v\n", yparser.GnmiPath2XPath(u.GetPath(), true), u.GetVal())
		updates = append(updates, u)
	}

	deletes := make([]*gnmi.Path, 0)
	for _, p := range n.GetDelete() {
		fmt.Printf("validateNotification diff delete old path: %s\n", yparser.GnmiPath2XPath(p, true))
		// workaround since the diff can return double pathElem
		_, p = validatePath(p)
		fmt.Printf("validateNotification diff delete new path: %s\n", yparser.GnmiPath2XPath(p, true))
		deletes = append(deletes, p)
	}
	return deletes, updates
}

// workaround for the diff handling
func validatePath(p *gnmi.Path) (bool, *gnmi.Path) {
	if len(p.GetElem()) <= 1 {
		return false, p
	}
	// when the 2nd last pathElem has a key and the last PathElem is an entry in the Key we should trim the last entry from the path
	// e.g. /interface[name=ethernet-1/49]/subinterface[index=1]/ipv4/address[ip-prefix=100.64.0.0/31]/ip-prefix, value: string_val:"100.64.0.0/31"
	// e.g. /interface[name=ethernet-1/49]/subinterface[index=1]/ipv4/address[ip-prefix=100.64.0.0/31]/ip-prefix, value: string_val:"100.64.0.0/31"
	if len(p.GetElem()[len(p.GetElem())-2].GetKey()) > 0 {
		if _, ok := p.GetElem()[len(p.GetElem())-2].GetKey()[p.GetElem()[len(p.GetElem())-1].GetName()]; ok {
			p.Elem = p.Elem[:len(p.GetElem())-1]
			return true, p
		}
	}
	return false, p
}

func getDiscoveryInfo(cacheTargetEntry *ygotnddtarget.NddTarget_TargetEntry) *targetv1.DiscoveryInfo {
	if cacheTargetEntry.GetState() == nil {
		return nil
	}
	return &targetv1.DiscoveryInfo{
		VendorType:         ygot.String(cacheTargetEntry.GetState().VendorType.String()),
		HostName:           ygot.String(*cacheTargetEntry.GetState().Hostname),
		Kind:               ygot.String(*cacheTargetEntry.GetState().Kind),
		SwVersion:          ygot.String(*cacheTargetEntry.GetState().SwVersion),
		MacAddress:         ygot.String(*cacheTargetEntry.GetState().MacAddress),
		SerialNumber:       ygot.String(*cacheTargetEntry.GetState().SerialNumber),
		SupportedEncodings: cacheTargetEntry.GetState().SupportedEncodings,
	}
}
