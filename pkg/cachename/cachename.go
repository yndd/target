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

package cachename

import "strings"

// namespacedName = <namespace.name>
// cacheNsName = <prefix.namespace.name>
// systemCachName = system.<namespace>.<targetname>
// configCachName = config.<namespace>.<targetname>
// targetCachName = target.<namespace>.<targetname>

const (
	SystemCachePrefix = "system"
	ConfigCachePrefix = "config"
	TargetCachePrefix = "target"
)

type NamespacedName string

// GetNameSpace returns the namespace from the namespacedName
func (t NamespacedName) GetNameSpace() string {
	split := strings.SplitN(string(t), ".", 2)
	if len(split) != 2 {
		return ""
	}
	return split[0]
}

// GetName returns the name from the namespacedName
func (t NamespacedName) GetName() string {
	split := strings.SplitN(string(t), ".", 2)
	if len(split) != 2 {
		return ""
	}
	return split[1]
}

// GetPrefixNamespacedName return a cacheNsName from prefix and namespacedName
func (t NamespacedName) GetPrefixNamespacedName(prefix string) string {
	return strings.Join([]string{prefix, string(t)}, ".")
}

// GetNamespacedName returns a namespacedName from namespace and name
func GetNamespacedName(namespace, name string) string {
	return strings.Join([]string{namespace, name}, ".")
}
