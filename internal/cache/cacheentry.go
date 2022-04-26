package cache

import (
	"errors"
	"fmt"

	"github.com/openconfig/ygot/ygot"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
)

type cacheEntry struct {
	id            string
	model         *model.Model
	runningConfig ygot.ValidatedGoStruct
	//systemCacheContent *ygotnddp.Device
}

func NewCacheEntry(id string) CacheEntry {
	ce := &cacheEntry{}
	ce.SetId(id)
	return ce
}

func (ce *cacheEntry) SetId(id string) {
	ce.id = id
}

func (ce *cacheEntry) GetId() string {
	return ce.id

}

func (ce *cacheEntry) GetModel() *model.Model {
	return ce.model
}

func (ce *cacheEntry) SetModel(m *model.Model) {
	ce.model = m
}

func (ce *cacheEntry) GetRunningConfig() ygot.ValidatedGoStruct {
	gostruct, err := ygot.DeepCopy(ce.runningConfig)
	if err != nil {
		return nil
	}
	return gostruct.(ygot.ValidatedGoStruct)
}

func (ce *cacheEntry) SetRunningConfig(c ygot.ValidatedGoStruct) {
	ce.runningConfig = c
}

func (ce *cacheEntry) IsValid() error {
	if ce.model == nil {
		return fmt.Errorf("cache entry %s: model reference is missing", ce.id)
	}
	return nil
}

func (ce *cacheEntry) GetSystemConfigMap() map[string]*ygotnddp.NddpSystem_Gvk {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return nil
	}
	return systemCache.Gvk
}

func (ce *cacheEntry) GetSystemConfigEntry(id string) (*ygotnddp.NddpSystem_Gvk, error) {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return nil, errors.New("wrong Device Object")
	}
	entry, exists := systemCache.Gvk[id]
	// raise error if the id does not exist
	if !exists {
		return nil, fmt.Errorf("system cache entry %s does not exist", id)
	}
	// return the data
	return entry, nil
}

func (ce *cacheEntry) AddSystemResourceEntry(id string, data *ygotnddp.NddpSystem_Gvk) error {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return errors.New("wrong Device Object")
	}
	// check if the given id already exists. if so, raise error to prevent overwite
	_, exists := systemCache.Gvk[id]
	if exists {
		return fmt.Errorf("system cache entry with id '%s' already exists", id)
	}
	// finally add the data to the map
	systemCache.Gvk[id] = data
	return nil
}

func (ce *cacheEntry) DeleteSystemConfigEntry(id string) error {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return errors.New("wrong Device Object")
	}
	// check if the given id already exists. if so, raise error to prevent overwite
	_, exists := systemCache.Gvk[id]
	if !exists {
		return fmt.Errorf("system cache entry with id '%s' not found", id)
	}
	// finally delete the data from the map
	delete(systemCache.Gvk, id)
	return nil
}

func (ce *cacheEntry) SetSystemResourceStatus(resourceName, reason string, status ygotnddp.E_NddpSystem_ResourceStatus) error {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return errors.New("wrong Device Object")
	}
	r, ok := systemCache.Gvk[resourceName]
	if !ok {
		return errors.New("resource not found")
	}

	if status == ygotnddp.NddpSystem_ResourceStatus_FAILED {
		*r.Attempt++
		// we dont update the status to failed unless we tried 3 times
		if *r.Attempt > 3 {
			r.Status = ygotnddp.NddpSystem_ResourceStatus_FAILED
			r.Reason = ygot.String(reason)
		}
	} else {
		// success
		r.Status = status
		r.Reason = ygot.String(reason)
	}

	systemCache.Gvk[resourceName] = r
	return nil
}

func (ce *cacheEntry) SetSystemExhausted(e uint32) error {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return errors.New("wrong Device Object")
	}

	systemCache.Cache.Exhausted = ygot.Uint32(e)
	if e != 0 {
		*systemCache.Cache.ExhaustedNbr++
	}

	return nil
}

func (ce *cacheEntry) GetSystemExhausted() (*uint32, error) {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return nil, errors.New("wrong Device Object")
	}
	return systemCache.Cache.Exhausted, nil
}

func (ce *cacheEntry) SetSystemCacheStatus(status bool) error {
	systemCache, ok := ce.runningConfig.(*ygotnddp.Device)
	if !ok {
		return errors.New("wrong Device Object")
	}
	systemCache.GetOrCreateCache().Update = ygot.Bool(status)
	return nil
}
