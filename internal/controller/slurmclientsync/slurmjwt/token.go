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
	"cmp"
	"context"
	"fmt"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jwt "github.com/golang-jwt/jwt/v5"
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/GoogleCloudPlatform/k8s-bridge/internal/utils/refresolver"
	slinkyv1beta1 "github.com/SlinkyProject/slurm-operator/api/v1beta1"
)

const (
	DefaultTokenKey = "token"
)

type Token struct {
	signingKey []byte
	method     jwt.SigningMethod
	username   string
	lifetime   time.Duration
}

func NewToken(signingKey []byte) *Token {
	return &Token{
		signingKey: signingKey,
		method:     jwt.SigningMethodHS256,
		username:   "slurm",
		lifetime:   time.Hour,
	}
}

// Slurm defines `lifetime=infinite` as this.
// Ref: https://github.com/SchedMD/slurm/blob/master/src/scontrol/scontrol.c#L1001
const slurmInfinite = math.MaxInt32 - 1

func clamp[T cmp.Ordered](val, a, b T) T {
	lower := min(a, b)
	upper := max(a, b)
	return min(max(val, lower), upper)
}

func (t *Token) WithLifetime(lifetime time.Duration) *Token {
	t.lifetime = clamp(lifetime, 0, slurmInfinite*time.Second)
	return t
}

func (t *Token) WithUsername(username string) *Token {
	t.username = username
	return t
}

// Ref: https://slurm.schedmd.com/jwt.html#compatibility
type TokenClaims struct {
	jwt.RegisteredClaims `json:",inline"`

	SlurmUsername string `json:"sun"`
}

func (t *Token) NewSignedToken() (string, error) {
	now := time.Now()
	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        string(uuid.NewUUID()),
			Issuer:    "slurm-operator",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.lifetime)),
			NotBefore: jwt.NewNumericDate(now),
		},
		SlurmUsername: t.username,
	}

	token := jwt.NewWithClaims(t.method, claims)

	tokenString, err := token.SignedString(t.signingKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

func ParseTokenClaims(tokenString string, signingKey []byte) (*TokenClaims, error) {
	signingKeyFunc := func(token *jwt.Token) (any, error) {
		return signingKey, nil
	}

	claims := &TokenClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, signingKeyFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return claims, nil
}

func VerifyToken(tokenString string, signingKey []byte) (bool, error) {
	signingKeyFunc := func(token *jwt.Token) (any, error) {
		return signingKey, nil
	}

	token, err := jwt.Parse(tokenString, signingKeyFunc)
	if err != nil {
		return false, fmt.Errorf("failed to parse JWT: %w", err)
	}

	return token.Valid, nil
}

// GenerateToken fetches the signing key from Slinky Controller's referenced Secret
// and returns a signed token, its expiration time, and the duration after which it should be enqueued for refresh.
func GenerateToken(ctx context.Context, k8sClient client.Client, namespace, controllerName string) (string, time.Time, time.Duration, error) {
	signingKey, err := GetSigningKey(ctx, k8sClient, namespace, controllerName)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	if len(signingKey) == 0 {
		return "", time.Time{}, 0, nil
	}

	lifetime := 15 * time.Minute
	refresh := lifetime * 4 / 5
	now := time.Now()
	expiresAt := now.Add(lifetime)

	authToken, err := NewToken(signingKey).
		WithLifetime(lifetime).
		NewSignedToken()
	if err != nil {
		return "", time.Time{}, 0, fmt.Errorf("failed to create Slurm auth token: %w", err)
	}

	return authToken, expiresAt, refresh, nil
}

func GetSigningKey(ctx context.Context, k8sClient client.Client, namespace, controllerName string) ([]byte, error) {
	resolver := refresolver.New(k8sClient)

	// Fetch Slinky Controller custom resource
	controller := &slinkyv1beta1.Controller{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: controllerName, Namespace: namespace}, controller); err != nil {
		return nil, fmt.Errorf("failed to get Slinky Controller %q: %w", controllerName, err)
	}

	return resolver.GetSecretKeyRef(ctx, controller.AuthJwtRef(), namespace)
}
