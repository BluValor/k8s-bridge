/*
Copyright 2026 Google LLC

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

package refresolver

import (
	"context"
	"fmt"

	slinkyv1beta1 "github.com/SlinkyProject/slurm-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RefResolver provides helper methods for resolving Kubernetes object references.
type RefResolver struct {
	reader client.Reader
}

// New creates and returns a new RefResolver.
func New(reader client.Reader) *RefResolver {
	return &RefResolver{
		reader: reader,
	}
}

// GetSecret fetches and returns the Secret referenced by LocalObjectReference in the given namespace.
func (r *RefResolver) GetSecret(ctx context.Context, ref corev1.LocalObjectReference, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      ref.Name,
	}
	if err := r.reader.Get(ctx, key, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// GetSecretKeyRef fetches the secret and extracts the key value referenced by SecretKeySelector.
func (r *RefResolver) GetSecretKeyRef(ctx context.Context, selector corev1.SecretKeySelector, namespace string) ([]byte, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Name:      selector.Name,
		Namespace: namespace,
	}
	if err := r.reader.Get(ctx, key, secret); err != nil {
		return nil, err
	}

	data, ok := secret.Data[selector.Key]
	if !ok {
		return nil, fmt.Errorf("secret key '%s' not found", selector.Key)
	}

	return data, nil
}

func IsKeyMatch(key1, key2 types.NamespacedName) bool {
	if key1.Namespace == key2.Namespace && key1.Name == key2.Name {
		return true
	}
	return false
}

func (r *RefResolver) GetRestapisForController(ctx context.Context, controller *slinkyv1beta1.Controller) (*slinkyv1beta1.RestApiList, error) {
	if controller == nil {
		return &slinkyv1beta1.RestApiList{}, nil
	}

	list := &slinkyv1beta1.RestApiList{}
	if err := r.reader.List(ctx, list, client.InNamespace(controller.Namespace)); err != nil {
		return nil, err
	}

	out := &slinkyv1beta1.RestApiList{}
	for _, item := range list.Items {
		refKey := types.NamespacedName{
			Namespace: item.Namespace,
			Name:      item.Spec.ControllerRef.Name,
		}
		if IsKeyMatch(refKey, client.ObjectKeyFromObject(controller)) {
			out.Items = append(out.Items, item)
		}
	}

	return out, nil
}
