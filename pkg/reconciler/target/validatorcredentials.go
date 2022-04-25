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
	"strings"

	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/resource"
	"github.com/yndd/ndd-target-runtime/pkg/ygotnddtarget"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// Errors
	errEmptyTargetSecretReference   = "empty target secret reference"
	errCredentialSecretDoesNotExist = "credential secret does not exist"
	errEmptyTargetAddress           = "empty target address"
	errMissingUsername              = "missing username in credentials"
	errMissingPassword              = "missing password in credentials"
)

// Credentials holds the information for authenticating with the Server.
type Credentials struct {
	Username string
	Password string
}

// getCredentials retrieve the Login details from the target cr spec and validates the target details.
// The target cr spec info is used to build the credentials for authentication to the target.
func (r *Reconciler) getCredentials(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) (creds *Credentials, err error) {
	//log := r.log.WithValues("namespace", t.GetNamespace(), "credentialsName", t.GetTargetCredentialsName(), "targetAddress", t.GetTargetAddress())
	//log.Debug("Credentials Validation")
	// Retrieve the secret from Kubernetes for this target
	credsSecret, err := r.getSecret(ctx, namespace, tspec)
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

	if creds.Username == "" {
		return nil, errors.New(errMissingUsername)
	}
	if creds.Password == "" {
		return nil, errors.New(errMissingPassword)
	}

	return creds, nil
}

// Retrieve the secret containing the credentials for authentiaction with the target.
func (r *Reconciler) getSecret(ctx context.Context, namespace string, tspec *ygotnddtarget.NddTarget_TargetEntry) (credsSecret *corev1.Secret, err error) {
	// check if credentialName is specified
	if *tspec.Config.CredentialName == "" {
		return nil, errors.New(errEmptyTargetSecretReference)
	}

	// check if credential secret exists
	secretKey := types.NamespacedName{
		Name:      *tspec.Config.CredentialName,
		Namespace: namespace,
	}
	credsSecret = &corev1.Secret{}
	if err := r.client.Get(ctx, secretKey, credsSecret); resource.IgnoreNotFound(err) != nil {
		return nil, errors.Wrap(err, errCredentialSecretDoesNotExist)
	}
	return credsSecret, nil
}
