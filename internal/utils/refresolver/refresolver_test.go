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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRefResolver_GetSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secretName := "test-secret"
	secretNamespace := "default"

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
		},
		Data: map[string][]byte{
			"token": []byte("test-token-value"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSecret).Build()
	resolver := New(fakeClient)
	ctx := context.Background()

	t.Run("successfully resolves existing secret", func(t *testing.T) {
		ref := corev1.LocalObjectReference{Name: secretName}
		secret, err := resolver.GetSecret(ctx, ref, secretNamespace)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if secret.Name != secretName {
			t.Errorf("expected secret name %q, got %q", secretName, secret.Name)
		}
		if string(secret.Data["token"]) != "test-token-value" {
			t.Errorf("expected token value %q, got %q", "test-token-value", string(secret.Data["token"]))
		}
	})

	t.Run("returns error when secret does not exist", func(t *testing.T) {
		ref := corev1.LocalObjectReference{Name: "non-existent-secret"}
		_, err := resolver.GetSecret(ctx, ref, secretNamespace)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
