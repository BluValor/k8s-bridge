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

package clientmap

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/SlinkyProject/slurm-client/pkg/client"
)

type clientEntry struct {
	cli        client.Client
	cancel     context.CancelFunc
	expiration time.Time
}

type ClientMap struct {
	lock    sync.RWMutex
	clients map[types.NamespacedName]clientEntry
}

func NewClientMap() *ClientMap {
	return &ClientMap{
		clients: make(map[types.NamespacedName]clientEntry),
	}
}

func (c *ClientMap) Get(name types.NamespacedName) client.Client {
	c.lock.RLock()
	defer c.lock.RUnlock()
	entry, ok := c.clients[name]
	if ok {
		return entry.cli
	}
	return nil
}

func (c *ClientMap) Has(names ...types.NamespacedName) bool {
	c.lock.RLock()
	defer c.lock.RUnlock()
	for _, name := range names {
		if _, ok := c.clients[name]; ok {
			return true
		}
	}
	return false
}

func (c *ClientMap) GetExpiration(name types.NamespacedName) time.Time {
	c.lock.RLock()
	defer c.lock.RUnlock()
	entry, ok := c.clients[name]
	if ok {
		return entry.expiration
	}
	return time.Time{}
}

func (c *ClientMap) GetClientAndExpiration(name types.NamespacedName) (client.Client, time.Time) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	if entry, ok := c.clients[name]; ok {
		return entry.cli, entry.expiration
	}
	return nil, time.Time{}
}

func (c *ClientMap) add(ctx context.Context, name types.NamespacedName, cli client.Client, expiration time.Time) bool {
	if _, ok := c.clients[name]; !ok {
		clientCtx, cancel := context.WithCancel(ctx)
		go func() {
			cli.Start(clientCtx)
		}()
		c.clients[name] = clientEntry{
			cli:        cli,
			cancel:     cancel,
			expiration: expiration,
		}
		return true
	}
	return false
}

func (c *ClientMap) Add(ctx context.Context, name types.NamespacedName, cli client.Client) bool {
	return c.AddWithExpiration(ctx, name, cli, time.Time{})
}

func (c *ClientMap) AddWithExpiration(ctx context.Context, name types.NamespacedName, cli client.Client, expiration time.Time) bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.remove(name)
	return c.add(ctx, name, cli, expiration)
}

func (c *ClientMap) remove(name types.NamespacedName) bool {
	if entry, ok := c.clients[name]; ok {
		entry.cancel()
		entry.cli.Stop()
		delete(c.clients, name)
		return true
	}
	return false
}

func (c *ClientMap) Remove(name types.NamespacedName) bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.remove(name)
}
