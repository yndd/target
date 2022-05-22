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

package targetcontroller

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sync"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	pkgmetav1 "github.com/yndd/ndd-core/apis/pkg/meta/v1"
	pkgv1 "github.com/yndd/ndd-core/apis/pkg/v1"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/ndd-target-runtime/internal/cache"
	"github.com/yndd/ndd-target-runtime/internal/targetchannel"
	"github.com/yndd/ndd-target-runtime/pkg/cachename"
	"github.com/yndd/ndd-target-runtime/pkg/grpcserver"
	"github.com/yndd/ndd-target-runtime/pkg/resource"
	"github.com/yndd/ndd-target-runtime/pkg/target"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"github.com/yndd/registrator/registrator"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// errors
	errCreateGnmiClient     = "cannot create gnmi client"
	errTargetInitFailed     = "cannot initialize the target"
	errTargetiscoveryFailed = "cannot discover target"
)

// TargetController defines the interfaces for the target controller
type TargetController interface {
	//targetinstance methods
	// add a target instance to the target controller
	AddTargetInstance(targetName string, t TargetInstance)
	// delete a target instance from the target controller
	DeleteTargetInstance(targetName string) error
	// get a target instance from the target controller
	GetTargetInstance(targetName string) TargetInstance
	// target controller methods
	// start the target controller
	Start() error
	// stop the target controller
	Stop() error
	// GetTargetChannel
	GetTargetChannel() chan targetchannel.TargetMsg
}

type Options struct {
	Logger logging.Logger
	//Scheme          *runtime.Scheme
	GrpcServerAddress string
	Registrator       registrator.Registrator
	//ServiceDiscovery          pkgmetav1.ServiceDiscoveryType
	//ServiceDiscoveryNamespace string
	//ControllerConfigName string
	TargetRegistry target.TargetRegistry
	TargetModel    *model.Model
}

// targetControllerImpl implements the TargetController interface
type targetControllerImpl struct {
	options *Options
	m       sync.RWMutex
	targets map[string]TargetInstance
	cache   cache.Cache
	//targetRegistry target.TargetRegistry
	//targetModel *model.Model
	grpcServerAddress string

	// channels
	targetCh chan targetchannel.TargetMsg
	stopCh   chan bool

	// kubernetes
	client   resource.ClientApplicator
	eventChs map[string]chan event.GenericEvent
	// server
	server grpcserver.GrpcServer
	// registrator
	registrator registrator.Registrator

	ctx context.Context
	cfn context.CancelFunc
	log logging.Logger
}

func New(ctx context.Context, config *rest.Config, o *Options) (TargetController, error) {
	log := o.Logger
	log.Debug("new target controller")

	c := &targetControllerImpl{
		options:           o,
		m:                 sync.RWMutex{},
		targets:           make(map[string]TargetInstance),
		targetCh:          make(chan targetchannel.TargetMsg),
		stopCh:            make(chan bool),
		registrator:       o.Registrator,
		grpcServerAddress: o.GrpcServerAddress,
	}

	// initialize the multi-device cache
	c.cache = cache.New()

	return c, nil
}

func (c *targetControllerImpl) GetTargetChannel() chan targetchannel.TargetMsg {
	return c.targetCh
}

func (c *targetControllerImpl) GetTargetInstance(targetName string) TargetInstance {
	c.m.Lock()
	defer c.m.Unlock()
	t, ok := c.targets[targetName]
	if !ok {
		return nil
	}
	return t
}

func (c *targetControllerImpl) AddTargetInstance(nsTargetName string, t TargetInstance) {
	c.m.Lock()
	defer c.m.Unlock()
	c.targets[nsTargetName] = t
}

func (c *targetControllerImpl) DeleteTargetInstance(nsTargetName string) error {
	c.m.Lock()
	defer c.m.Unlock()
	if ti, ok := c.targets[nsTargetName]; ok {
		if err := ti.StopTargetCollector(); err != nil {
			return err
		}
		if err := ti.StopTargetReconciler(); err != nil {
			return err
		}
		ti.DeRegister()
	}
	delete(c.targets, nsTargetName)
	targetCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	c.cache.DeleteEntry(targetCacheNsTargetName)
	return nil
}

func (c *targetControllerImpl) Stop() error {
	c.log.Debug("stopping  deviceDriver...")

	close(c.stopCh)
	return nil
}

func (c *targetControllerImpl) Start() error {
	c.log.Debug("starting targetdriver...")

	// start grpc server
	c.server = grpcserver.New(c.grpcServerAddress,
		grpcserver.WithHealth(true),
		grpcserver.WithGnmi(true),
		grpcserver.WithCache(c.cache),
		grpcserver.WithLogger(c.log),
		grpcserver.WithTargetChannel(c.targetCh),
	)
	if err := c.server.Start(); err != nil {
		return err
	}

	// register the service
	c.registrator.Register(c.ctx, &registrator.Service{
		Name:    os.Getenv("SERVICE_NAME"),
		ID:      os.Getenv("POD_NAME"),
		Port:    pkgmetav1.GnmiServerPort,
		Address: os.Getenv("POD_IP"),
		//Address:    strings.Join([]string{os.Getenv("POD_NAME"), os.Getenv("GRPC_SVC_NAME"), os.Getenv("POD_NAMESPACE"), "svc", "cluster", "local"}, "."),
		Tags:         pkgv1.GetServiceTag(os.Getenv("POD_NAMESPACE"), os.Getenv("POD_NAME")),
		HealthChecks: []registrator.HealthKind{registrator.HealthKindTTL, registrator.HealthKindGRPC},
	})

	// start target handler, which enables crud operations for targets
	// create, delete requests
	go c.startTargetWorker(c.ctx)

	return nil
}

func (c *targetControllerImpl) startTargetWorker(ctx context.Context) {
	c.log.Debug("Starting targetChangeHandler...")

	for {
		select {
		case <-ctx.Done():
			c.log.Debug("target controller worker stopped", "error", ctx.Err())
			return
		case <-c.stopCh:
			c.log.Debug("target controller worker stopped")
			return
		case t := <-c.targetCh:
			log := c.log.WithValues("targetFullName", t.Target)
			switch t.Operation {
			case targetchannel.Start:
				if err := c.startTarget(t.Target); err != nil {
					log.Debug("target init/start failed", "error", err)

					// delete the context since it is not ok to connect to the device
					if err := c.DeleteTargetInstance(t.Target); err != nil {
						log.Debug("target delete failed", "error", err)
					}
				} else {
					log.Debug("target init/start success")
				}
			case targetchannel.Stop:
				// stop the target and delete the target from the targetlist
				if err := c.stopTarget(t.Target); err != nil {
					log.Debug("target stop failed", "error", err)
					if err := c.DeleteTargetInstance(t.Target); err != nil {
						log.Debug("target delete failed", "error", err)
					}
				} else {
					c.log.Debug("target stop success")
					if err := c.DeleteTargetInstance(t.Target); err != nil {
						c.log.Debug("target delete failed", "error", err)
					}
				}
			}
		}
	}
}

func (c *targetControllerImpl) startTarget(nsTargetName string) error {
	log := c.log.WithValues("nsTargetName", nsTargetName)
	log.Debug("start target...")
	// the target we get on the channel has <namespace.target> semantics
	targetName := cachename.NamespacedName(nsTargetName).GetName()
	namespace := cachename.NamespacedName(nsTargetName).GetNameSpace()

	targetCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	if !c.cache.Exists(targetCacheNsTargetName) {
		return fmt.Errorf("target cache not initialized: %s", targetCacheNsTargetName)
	}

	ti, err := NewTargetInstance(c.ctx, namespace, nsTargetName, targetName,
		WithTargetInstanceCache(c.cache),
		WithTargetInstanceClient(c.client),
		WithTargetInstanceLogger(c.log),
		WithTargetInstanceEventCh(c.eventChs),
		WithTargetInstanceTargetsRegistry(c.options.TargetRegistry),
		WithTargetInstanceRegistrator(c.registrator),
	)
	if err != nil {
		return err
	}
	c.AddTargetInstance(nsTargetName, ti)

	// get capabilities the device
	//cap, err := ti.GetCapabilities()
	//if err != nil {
	//	return err
	//}
	//enc := target.GetSupportedEncodings(cap)
	// discover the target
	//if err := ti.Discover(enc); err != nil {
	//	return err
	//}
	// update model data from capabilities to the targetmodel
	//c.targetModel.ModelData = model.GetModelData(cap.SupportedModels)

	// initialize the config target cache
	configCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.ConfigCachePrefix)
	cce := cache.NewCacheEntry(configCacheNsTargetName)
	cce.SetModel(c.options.TargetModel)
	c.cache.AddEntry(cce)

	// initialize the system target cache
	systemCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.SystemCachePrefix)
	sce := cache.NewCacheEntry(systemCacheNsTargetName)
	sce.SetModel(&model.Model{
		ModelData:       []*gnmi.ModelData{},
		StructRootType:  reflect.TypeOf((*ygotnddp.Device)(nil)),
		SchemaTreeRoot:  ygotnddp.SchemaTree["Device"],
		JsonUnmarshaler: ygotnddp.Unmarshal,
		EnumData:        ygotnddp.ΛEnum,
	})
	c.cache.AddEntry(sce)

	if err := ti.GetInitialTargetConfig(); err != nil {
		c.log.Debug("initialize target config", "error", err)
		return errors.Wrap(err, "cannot get or initialize initial config")
	}

	if err := ti.InitializeSystemConfig(); err != nil {
		c.log.Debug("initialize system config", "error", err)
		return errors.Wrap(err, "cannot validate system config")
	}

	if err := ti.StartTargetReconciler(); err != nil {
		c.log.Debug("start target reconciler", "error", err)
		return errors.Wrap(err, "cannot start target reconciler")
	}

	if err := ti.StartTargetCollector(); err != nil {
		c.log.Debug("start target collector", "error", err)
		return errors.Wrap(err, "cannot start target collector")
	}

	ti.Register()

	return nil
}

func (c *targetControllerImpl) stopTarget(nsTargetName string) error {
	// delete the target instance -> stops the collectors, reconciler
	c.DeleteTargetInstance(nsTargetName)

	// clear the cache from the device
	targetCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	c.cache.DeleteEntry(targetCacheNsTargetName)
	return nil
}
