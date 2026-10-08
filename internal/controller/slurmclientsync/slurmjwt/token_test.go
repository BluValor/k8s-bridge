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

package slurmjwt

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slinkyv1beta1 "github.com/SlinkyProject/slurm-operator/api/v1beta1"
)

const (
	DefaultSigningKeyLength = 1024
)

func NewSigningKey() []byte {
	return NewSigningKeyWithLength(DefaultSigningKeyLength)
}

func NewSigningKeyWithLength(length int) []byte {
	key := make([]byte, length)
	if _, err := rand.Read(key); err != nil {
		// rand.Read() signature returns an error, but in practice it never will
		// because it internally panics on error.
		//
		// Should a non-nil error ever be returned we panic here to ensure we
		// never return a dubious value on accident.
		panic(err)
	}
	return key
}

func newSignedToken(signingKey []byte) string {
	tokenString, err := NewToken(signingKey).NewSignedToken()
	if err != nil {
		panic(err)
	}
	return tokenString
}

func TestToken_NewSignedToken(t *testing.T) {
	type fields struct {
		token *Token
	}
	tests := []struct {
		name    string
		fields  fields
		wantOk  bool
		wantErr bool
	}{
		{
			name: "Generate",
			fields: fields{
				token: NewToken(NewSigningKey()),
			},
			wantOk: true,
		},
		{
			name: "With Options",
			fields: fields{
				token: NewToken(NewSigningKey()).
					WithUsername("foo").
					WithLifetime(30 * time.Second),
			},
			wantOk: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := tt.fields.token
			got, err := tr.NewSignedToken()
			if (err != nil) != tt.wantErr {
				t.Errorf("Token.NewSignedToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			ok, err := VerifyToken(got, tr.signingKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyToken() = %v", err)
				return
			}
			if ok != tt.wantOk {
				t.Errorf("VerifyToken() ok = %v, wantOk %v", ok, tt.wantOk)
			}
		})
	}
}

func TestParseTokenClaims(t *testing.T) {
	signingKey := NewSigningKey()
	type args struct {
		tokenString string
		signingKey  []byte
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "empty",
			args: args{
				tokenString: newSignedToken(signingKey),
				signingKey:  signingKey,
			},
		},
		{
			name: "different signingKey",
			args: args{
				tokenString: newSignedToken(signingKey),
				signingKey:  nil,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTokenClaims(tt.args.tokenString, tt.args.signingKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTokenClaims() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestVerifyToken(t *testing.T) {
	signingKey := NewSigningKey()
	otherSigningKey := NewSigningKey()
	type args struct {
		tokenString string
		signingKey  []byte
	}
	tests := []struct {
		name        string
		args        args
		wantOk      bool
		expectedErr error
	}{
		{
			name: "Empty Token",
			args: args{
				tokenString: "",
				signingKey:  signingKey,
			},
			expectedErr: jwt.ErrTokenMalformed,
		},
		{
			name: "Valid",
			args: args{
				tokenString: newSignedToken(signingKey),
				signingKey:  signingKey,
			},
			wantOk: true,
		},
		{
			name: "Different SigningKey",
			args: args{
				tokenString: newSignedToken(signingKey),
				signingKey:  otherSigningKey,
			},
			expectedErr: jwt.ErrTokenSignatureInvalid,
		},
		{
			name: "Malformed Token",
			args: args{
				tokenString: "this-is-not-a-jwt",
				signingKey:  signingKey,
			},
			expectedErr: jwt.ErrTokenMalformed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyToken(tt.args.tokenString, tt.args.signingKey)
			if tt.expectedErr != nil {
				if !errors.Is(err, tt.expectedErr) {
					t.Errorf("VerifyToken() error = %v, expectedErr %v", err, tt.expectedErr)
				}
			} else if err != nil {
				t.Errorf("VerifyToken() unexpected error = %v", err)
			}
			if ok != tt.wantOk {
				t.Errorf("VerifyToken() ok = %v, wantOk %v", ok, tt.wantOk)
			}
		})
	}
}

func TestGenerateToken(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = slinkyv1beta1.AddToScheme(scheme)

	namespace := "default"
	controllerName := "my-controller"
	signingKey := NewSigningKey()

	validSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controllerName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			DefaultTokenKey: signingKey,
		},
	}

	fallbackSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controllerName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"jwt.key": signingKey,
		},
	}

	controllerWithDefaultKey := &slinkyv1beta1.Controller{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controllerName,
			Namespace: namespace,
		},
	}
	controllerWithDefaultKey.Spec.JwtKeyRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: controllerName,
		},
		Key: DefaultTokenKey,
	}

	controllerWithFallbackKey := &slinkyv1beta1.Controller{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controllerName,
			Namespace: namespace,
		},
	}
	controllerWithFallbackKey.Spec.JwtKeyRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: controllerName,
		},
		Key: "jwt.key",
	}

	validControllerWithCustomKey := &slinkyv1beta1.Controller{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controllerName,
			Namespace: namespace,
		},
	}
	validControllerWithCustomKey.Spec.JwtKeyRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "my-secret",
		},
		Key: "my-key",
	}

	customSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-secret",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"my-key": signingKey,
		},
	}

	tests := []struct {
		name           string
		objects        []client.Object
		wantErr        bool
		wantEmptyToken bool
	}{
		{
			name: "Success with default key",
			objects: []client.Object{
				validSecret,
				controllerWithDefaultKey,
			},
			wantErr: false,
		},
		{
			name: "Success with fallback key",
			objects: []client.Object{
				fallbackSecret,
				controllerWithFallbackKey,
			},
			wantErr: false,
		},
		{
			name: "Success with custom key ref",
			objects: []client.Object{
				customSecret,
				validControllerWithCustomKey,
			},
			wantErr: false,
		},
		{
			name: "Controller not found",
			objects: []client.Object{
				validSecret,
			},
			wantErr: true,
		},
		{
			name: "Secret not found",
			objects: []client.Object{
				controllerWithDefaultKey,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			token, _, refresh, err := GenerateToken(context.Background(), fakeClient, namespace, controllerName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tt.wantEmptyToken && token != "" {
					t.Errorf("GenerateToken() expected empty token, got %v", token)
				}
				if !tt.wantEmptyToken {
					if token == "" {
						t.Errorf("GenerateToken() expected valid token, got empty")
					}
					if refresh <= 0 {
						t.Errorf("GenerateToken() expected refresh duration > 0, got %v", refresh)
					}
				}
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name string
		val  int
		a    int
		b    int
		want int
	}{
		{"in range", 5, 0, 10, 5},
		{"below a", -1, 0, 10, 0},
		{"above b", 15, 0, 10, 10},
		{"inverted bounds in range", 5, 10, 0, 5},
		{"inverted bounds below a", -1, 10, 0, 0},
		{"inverted bounds above b", 15, 10, 0, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clamp(tt.val, tt.a, tt.b); got != tt.want {
				t.Errorf("clamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToken_Claims(t *testing.T) {
	signingKey := NewSigningKey()
	token := NewToken(signingKey).
		WithUsername("test-user").
		WithLifetime(30 * time.Minute)

	tokenString, err := token.NewSignedToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	claims, err := ParseTokenClaims(tokenString, signingKey)
	if err != nil {
		t.Fatalf("Failed to parse token claims: %v", err)
	}

	if claims.SlurmUsername != "test-user" {
		t.Errorf("Expected username 'test-user', got %v", claims.SlurmUsername)
	}

	if claims.ExpiresAt == nil {
		t.Fatalf("Missing exp claim")
	}

	if claims.IssuedAt == nil {
		t.Fatalf("Missing iat claim")
	}

	duration := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if duration != 30*time.Minute {
		t.Errorf("Expected lifetime of 30 minutes, got %v", duration)
	}
}
