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
	"encoding/json"
	"reflect"
	"strings"
	"time"

	gnmictarget "github.com/karimra/gnmic/target"
	"github.com/karimra/gnmic/types"
	"github.com/openconfig/ygot/ygot"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/ndd-runtime/pkg/utils"
	targetv1 "github.com/yndd/ndd-target-runtime/apis/dvr/v1"
	"github.com/yndd/ndd-target-runtime/internal/cache"
	"github.com/yndd/ndd-target-runtime/internal/targetcollector"
	"github.com/yndd/ndd-target-runtime/internal/targetreconciler"
	"github.com/yndd/ndd-target-runtime/pkg/cachename"
	"github.com/yndd/ndd-target-runtime/pkg/resource"
	"github.com/yndd/ndd-target-runtime/pkg/target"
	"github.com/yndd/ndd-target-runtime/pkg/ygotnddtarget"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"github.com/yndd/registrator/registrator"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// Timers
	defaultTimeout = 5 * time.Second
	// Errors
	errTargetNotRegistered          = "the target type is not registered"
	errEmptyTargetSecretReference   = "empty target secret reference"
	errCredentialSecretDoesNotExist = "credential secret does not exist"
	errEmptyTargetAddress           = "empty target address"
	errMissingUsername              = "missing username in credentials"
	errMissingPassword              = "missing password in credentials"
)

// TargetInstanceOption can be used to manipulate TargetInstance config.
type TargetInstanceOption func(TargetInstance)

// TargetInstance defines the interfaces for the target instance
type TargetInstance interface {
	// Options
	// add a logger to the Target Instance
	WithTargetInstanceLogger(l logging.Logger)
	// add a k8s client to the Target Instance
	WithTargetInstanceClient(c resource.ClientApplicator)
	// add a cache to the Target Instance
	WithTargetInstanceCache(c cache.Cache)
	// add an event channel to Target Instance
	WithTargetInstanceEventCh(eventChs map[string]chan event.GenericEvent)
	// add the target registry to Target Instance
	WithTargetInstanceTargetsRegistry(target.TargetRegistry)
	// add the registrator to Target Instance
	WithTargetInstanceRegistrator(r registrator.Registrator)

	// Methods
	// CreateGNMIClient create a gnmi client for the target
	CreateGNMIClient() error
	// GetCapabilities retrieves the capabilities of the target
	//GetCapabilities() (*gnmi.CapabilityResponse, error)
	// GetRunningConfig retrieves the target running config
	//GetRunningConfig() error
	// GetInitialTargetConfig retrieves the initial target instance
	GetInitialTargetConfig() error
	// InitializeSystemConfig initializes the target system config
	InitializeSystemConfig() error
	// StartTargetReconciler starts the target reconciler per target
	StartTargetReconciler() error
	// StartTargetCollector starts the target collector per target
	StartTargetCollector() error
	// StopTargetReconciler stops the target reconciler per target
	StopTargetReconciler() error
	// StopTargetCollector stops the target collector per target
	StopTargetCollector() error
	// Regsiter
	Register()
	// DeRegister
	DeRegister()
}

// WithTargetInstanceLogger adds a logger to the target instance
func WithTargetInstanceLogger(l logging.Logger) TargetInstanceOption {
	return func(o TargetInstance) {
		o.WithTargetInstanceLogger(l)
	}
}

// WithTargetInstanceClient adds a k8s client to the target instance
func WithTargetInstanceClient(c resource.ClientApplicator) TargetInstanceOption {
	return func(o TargetInstance) {
		o.WithTargetInstanceClient(c)
	}
}

// WithTargetInstanceCache adds a cache to the target instance
func WithTargetInstanceCache(c cache.Cache) TargetInstanceOption {
	return func(s TargetInstance) {
		s.WithTargetInstanceCache(c)
	}
}

// WithTargetInstanceEventCh adds event channels to the target instance
func WithTargetInstanceEventCh(eventChs map[string]chan event.GenericEvent) TargetInstanceOption {
	return func(s TargetInstance) {
		s.WithTargetInstanceEventCh(eventChs)
	}
}

// WithTargetInstanceTargetsRegistry adds target registry to the target instance
func WithTargetInstanceTargetsRegistry(tr target.TargetRegistry) TargetInstanceOption {
	return func(s TargetInstance) {
		s.WithTargetInstanceTargetsRegistry(tr)
	}
}

func WithTargetInstanceRegistrator(r registrator.Registrator) TargetInstanceOption {
	return func(s TargetInstance) {
		s.WithTargetInstanceRegistrator(r)
	}
}

type targetInstance struct {
	// kubernetes
	client   resource.ClientApplicator
	eventChs map[string]chan event.GenericEvent

	// tartgetRegistry
	targetRegistry target.TargetRegistry

	// controller info
	controllerName string
	// target info
	newTarget       func() targetv1.Tg
	nsTargetName    string
	targetName      string
	namespace       string
	gnmicTarget     *gnmictarget.Target
	paths           []*string
	cache           cache.Cache
	target          target.Target // implements specifics for the vendor type, like srl or sros
	targetModel     *model.Model
	targetFullModel *model.Model
	collector       targetcollector.Collector
	reconciler      targetreconciler.Reconciler
	// dynamic discovered data
	//discoveryInfo *targetv1.DiscoveryInfo
	initialConfig interface{}
	// registrator
	registrator registrator.Registrator
	// chan
	stopCh chan struct{} // used to stop the child go routines if the target gets deleted
	ctx    context.Context
	cfn    context.CancelFunc
	// logging
	log logging.Logger
}

func NewTargetInstance(ctx context.Context, controllerName, namespace, nsTargetName, targetName string, opts ...TargetInstanceOption) (TargetInstance, error) {
	tg := func() targetv1.Tg { return &targetv1.Target{} }

	ti := &targetInstance{
		controllerName: controllerName,
		nsTargetName:   nsTargetName,
		targetName:     targetName,
		namespace:      namespace,
		paths:          []*string{utils.StringPtr("/")},
		stopCh:         make(chan struct{}),
		newTarget:      tg,
		targetModel: &model.Model{
			StructRootType:  reflect.TypeOf((*ygotnddtarget.NddTarget_TargetEntry)(nil)),
			SchemaTreeRoot:  ygotnddtarget.SchemaTree["NddTarget_TargetEntry"],
			JsonUnmarshaler: ygotnddtarget.Unmarshal,
			EnumData:        ygotnddtarget.ΛEnum,
		},
	}

	for _, opt := range opts {
		opt(ti)
	}

	ti.ctx, ti.cfn = context.WithCancel(ctx)

	// create gnmi client
	if err := ti.CreateGNMIClient(); err != nil {
		return nil, err
	}

	// initialize the target which implements the specific gnmi calls for this vendor target type
	var err error
	ti.target, err = ti.targetRegistry.Initialize(ygotnddtarget.NddTarget_VendorType_nokia_srl)
	if err != nil {
		return nil, err
	}
	if err := ti.target.Init(
		target.WithLogging(ti.log.WithValues("target", ti.targetName)),
		target.WithTarget(ti.gnmicTarget),
	); err != nil {
		return nil, err
	}

	return ti, nil
}

func (ti *targetInstance) WithTargetInstanceLogger(l logging.Logger) {
	ti.log = l
}

func (ti *targetInstance) WithTargetInstanceClient(c resource.ClientApplicator) {
	ti.client = c
}

func (ti *targetInstance) WithTargetInstanceCache(c cache.Cache) {
	ti.cache = c
}

func (ti *targetInstance) WithTargetInstanceEventCh(eventChs map[string]chan event.GenericEvent) {
	ti.eventChs = eventChs
}

func (ti *targetInstance) WithTargetInstanceTargetsRegistry(tr target.TargetRegistry) {
	ti.targetRegistry = tr
}

func (ti *targetInstance) WithTargetInstanceRegistrator(r registrator.Registrator) {
	ti.registrator = r
}

func (ti *targetInstance) CreateGNMIClient() error {
	targetConfig, err := ti.getTargetConfig()
	if err != nil {
		return err
	}
	ti.gnmicTarget = gnmictarget.NewTarget(targetConfig)
	if err := ti.gnmicTarget.CreateGNMIClient(ti.ctx, grpc.WithBlock()); err != nil { // TODO add dialopts
		return errors.Wrap(err, errCreateGnmiClient)
	}
	return nil
}

/*
func (ti *targetInstance) GetCapabilities() (*gnmi.CapabilityResponse, error) {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	return ti.target.GNMICap(ti.ctx)
}
*/

/*
func (ti *targetInstance) GetRunningConfig() error {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	log.Debug("GetRunningConfig...")
	// get device details through gnmi
	var err error

	// query the target cache
	targetCacheNsTargetName := cachename.NamespacedName(ti.nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	ce, err := ti.cache.GetEntry(targetCacheNsTargetName)
	if err != nil {
		return err
	}
	targetCacheEntry, ok := ce.GetRunningConfig().(*ygotnddtarget.NddTarget_TargetEntry)
	if !ok {
		return errors.New("unexpected Object")
	}

	ce.SetRunningConfig(targetCacheEntry)
	return nil
}
*/

func (ti *targetInstance) GetInitialTargetConfig() error {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	// get initial config through gnmi
	var err error
	ti.initialConfig, err = ti.target.GetConfig(ti.ctx)
	if err != nil {
		return err
	}

	//log.Debug("initial config", "config", ddd.initialConfig)
	config, err := json.Marshal(ti.initialConfig)
	if err != nil {
		return err
	}

	//fmt.Println(string(config))
	configCacheNsTargetName := cachename.NamespacedName(ti.nsTargetName).GetPrefixNamespacedName(cachename.ConfigCachePrefix)
	ce, err := ti.cache.GetEntry(configCacheNsTargetName)
	if err != nil {
		log.Debug("Get Device data from cache", "error", err)
	}

	rootStruct, err := ce.GetModel().NewConfigStruct(config, true)
	if err != nil {
		ti.log.Debug("NewConfigStruct Device config error", "error", err)
		return err
	}
	if config != nil {
		ce.SetRunningConfig(rootStruct)
	}
	return nil
}

func (ti *targetInstance) InitializeSystemConfig() error {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	systemCacheNsTargetName := cachename.NamespacedName(ti.nsTargetName).GetPrefixNamespacedName(cachename.SystemCachePrefix)

	nddpData := &ygotnddp.Device{
		Cache: &ygotnddp.NddpSystem_Cache{
			Update:       ygot.Bool(false),
			Exhausted:    ygot.Uint32(0),
			ExhaustedNbr: ygot.Uint64(0),
		},
	}

	nddpJson, err := ygot.EmitJSON(nddpData, &ygot.EmitJSONConfig{
		Format: ygot.RFC7951,
	})
	if err != nil {
		return err
	}

	var ce cache.CacheEntry
	if ce, err = ti.cache.GetEntry(systemCacheNsTargetName); err != nil {
		log.Debug("unable to get cache entry", "error", err)
		return err
	}
	model := ce.GetModel()
	rootStruct, err := model.NewConfigStruct([]byte(nddpJson), true)
	if err != nil {
		log.Debug("NewConfigStruct System config error", "error", err)
		return err
	}
	if []byte(nddpJson) != nil {
		ce.SetRunningConfig(rootStruct)
	}

	return nil
}

func (ti *targetInstance) StartTargetReconciler() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	// start per target reconciler
	var err error
	ti.reconciler, err = targetreconciler.New(ti.gnmicTarget.Config, ti.namespace,
		targetreconciler.WithTarget(ti.gnmicTarget),
		targetreconciler.WithCache(ti.cache),
		targetreconciler.WithLogger(ti.log),
	)
	if err != nil {
		return errors.Wrap(err, "cannot start target reconciler")
	}
	// debug
	ti.reconciler.Start()
	return nil
}

func (ti *targetInstance) StopTargetReconciler() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	if ti.reconciler != nil {
		return ti.reconciler.Stop()
	}
	return nil
}

func (ti *targetInstance) StartTargetCollector() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	// start per target collector
	var err error
	ti.collector, err = targetcollector.New(ti.ctx, ti.gnmicTarget.Config, ti.namespace, ti.paths,
		targetcollector.WithCache(ti.cache),
		targetcollector.WithLogger(ti.log),
		targetcollector.WithEventCh(ti.eventChs),
	)
	if err != nil {
		return errors.Wrap(err, "cannot start device collector")
	}
	ti.collector.Start()
	return nil
}

func (ti *targetInstance) StopTargetCollector() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	if ti.collector != nil {
		return ti.collector.Stop()
	}
	return nil
}

func (ti *targetInstance) getTargetConfig() (*types.TargetConfig, error) {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	t := ti.newTarget()
	if err := ti.client.Get(ti.ctx, k8stypes.NamespacedName{
		Namespace: ti.namespace,
		Name:      ti.targetName,
	}, t); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		log.Debug("cannot get target from k8s api", "error", err)
		return nil, err
	}

	tspec, err := t.GetSpec()
	if err != nil {
		log.Debug("Cannot get spec", "error", err)
		return nil, err
	}

	creds, err := ti.getCredentials(ti.ctx, tspec)
	if err != nil {
		log.Debug("Cannot get credentials", "error", err)
		return nil, err
	}

	return &types.TargetConfig{
		Name:       ti.targetName,
		Address:    *tspec.GetConfig().Address,
		Username:   &creds.Username,
		Password:   &creds.Password,
		Timeout:    defaultTimeout,
		Insecure:   tspec.GetConfig().Insecure,
		SkipVerify: tspec.GetConfig().SkipVerify,
		TLSCA:      utils.StringPtr(""), //TODO TLS
		TLSCert:    utils.StringPtr(""), //TODO TLS
		TLSKey:     utils.StringPtr(""), //TODO TLS
		Gzip:       utils.BoolPtr(false),
	}, nil
}

// Credentials holds the information for authenticating with the Server.
type Credentials struct {
	Username string
	Password string
}

// getCredentials retrieve the Login details from the target cr spec and validates the target details.
// The target cr spec info is used to build the credentials for authentication to the target.
func (ti *targetInstance) getCredentials(ctx context.Context, tspec *ygotnddtarget.NddTarget_TargetEntry) (creds *Credentials, err error) {
	// Retrieve the secret from Kubernetes for thistarget
	credsSecret, err := ti.getSecret(ctx, tspec)
	if err != nil {
		return nil, err
	}

	// Check if address is defined on the target cr
	if *tspec.Config.Address == "" {
		return nil, errors.New(errEmptyTargetAddress)
	}

	creds = &Credentials{
		Username: strings.TrimSuffix(string(credsSecret.Data["username"]), "\n"),
		Password: strings.TrimSuffix(string(credsSecret.Data["password"]), "\n"),
	}

	//log.Debug("Credentials", "creds", creds)

	if creds.Username == "" {
		return nil, errors.New(errMissingUsername)
	}
	if creds.Password == "" {
		return nil, errors.New(errMissingPassword)
	}

	return creds, nil
}

// Retrieve the secret containing the credentials for authentiaction with the target.
func (ti *targetInstance) getSecret(ctx context.Context, tspec *ygotnddtarget.NddTarget_TargetEntry) (credsSecret *corev1.Secret, err error) {
	// check if credentialName is specified
	if *tspec.Config.CredentialName == "" {
		return nil, errors.New(errEmptyTargetSecretReference)
	}

	// check if credential secret exists
	secretKey := k8stypes.NamespacedName{
		Name:      *tspec.Config.CredentialName,
		Namespace: ti.namespace,
	}
	credsSecret = &corev1.Secret{}
	if err := ti.client.Get(ctx, secretKey, credsSecret); resource.IgnoreNotFound(err) != nil {
		return nil, errors.Wrap(err, errCredentialSecretDoesNotExist)
	}
	return credsSecret, nil
}

func (ti *targetInstance) Register() {
	ti.registrator.Register(ti.ctx, &registrator.Service{
		ID:         strings.Join([]string{ti.controllerName, "worker", "target"}, "-"),
		Name:       ti.nsTargetName,
		HealthKind: registrator.HealthKindNone,
	})
}

func (ti *targetInstance) DeRegister() {
	ti.registrator.DeRegister(ti.ctx, strings.Join([]string{ti.controllerName, "worker", "target"}, "-"))
}
