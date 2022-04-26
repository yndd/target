package cache

import (
	"fmt"
	"sync"

	"github.com/openconfig/ygot/ygot"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
)

// yangcache "github.com/yndd/ndd-yang/pkg/cache"

type Cache interface {
	Lock()
	Unlock()
	Exists(id string) bool
	GetEntry(id string) (CacheEntry, error)
	AddEntry(CacheEntry) error
	DeleteEntry(id string) error
}

type cache struct {
	m       sync.RWMutex
	content map[string]CacheEntry
}

type CacheEntry interface {
	GetId() string
	GetModel() *model.Model
	GetRunningConfig() ygot.ValidatedGoStruct

	SetId(string)
	SetModel(*model.Model)
	SetRunningConfig(ygot.ValidatedGoStruct)

	GetSystemConfigMap() map[string]*ygotnddp.NddpSystem_Gvk
	GetSystemConfigEntry(id string) (*ygotnddp.NddpSystem_Gvk, error)
	AddSystemResourceEntry(id string, data *ygotnddp.NddpSystem_Gvk) error
	DeleteSystemConfigEntry(id string) error
	SetSystemResourceStatus(resourceName, reason string, status ygotnddp.E_NddpSystem_ResourceStatus) error

	SetSystemExhausted(e uint32) error
	GetSystemExhausted() (*uint32, error)

	SetSystemCacheStatus(status bool) error

	// checks that model, running config and system config are set.
	IsValid() error
}

type SystemCacheEntry interface {
	GetMap() map[string]*ygotnddp.NddpSystem_Gvk
	Get(id string) (*ygotnddp.NddpSystem_Gvk, error)
	Add(id string, data *ygotnddp.NddpSystem_Gvk) error
}

func New() Cache {
	return &cache{
		content: map[string]CacheEntry{},
	}
}

func (c *cache) Lock() {
	c.m.Lock()
}

func (c *cache) Unlock() {
	c.m.Unlock()
}

func (c *cache) Exists(id string) bool {
	_, exists := c.content[id]
	return exists
}

func (c *cache) AddEntry(ce CacheEntry) error {
	ceid := ce.GetId()
	// make sure the CacheEntry is valid, all required information is present.
	if err := ce.IsValid(); err != nil {
		return fmt.Errorf("failed adding cache entry to cache [%s]", err)
	}
	// make sure we are not overwriting existing cache entries
	if _, exists := c.content[ceid]; exists {
		return fmt.Errorf("cache entry with id '%s' already exists", ceid)
	}
	// add entry to cache
	c.content[ceid] = ce
	return nil
}

func (c *cache) DeleteEntry(id string) error {
	// Throw error if Id of the to be removed entry does not exist
	if _, exists := c.content[id]; !exists {
		return fmt.Errorf("cache entry with id '%s' does not exist in cache", id)
	}
	// finally delete the entry
	delete(c.content, id)
	return nil
}

func (c *cache) GetEntry(id string) (CacheEntry, error) {
	var ce CacheEntry
	var exists bool
	// throw error if entry does not exist
	if ce, exists = c.content[id]; !exists {
		return nil, fmt.Errorf("cache entry '%s' does not exist", id)
	}
	// If entry exist return it
	return ce, nil
}
