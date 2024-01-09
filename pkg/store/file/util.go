// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package file

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/iptecharch/config-server/pkg/store"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func (r *file[T1]) filename(key store.Key) string {
	if key.Namespace != "" {
		return filepath.Join(r.objRootPath, key.Namespace, key.Name+".json")
	}
	return filepath.Join(r.objRootPath, key.Name+".json")
}

func (r *file[T1]) readFile(ctx context.Context, key store.Key) (T1, error) {
	var obj T1
	content, err := os.ReadFile(filepath.Clean(r.filename(key)))
	if err != nil {
		return obj, err
	}
	newObj := r.newFunc()
	decodeObj, _, err := r.codec.Decode(content, nil, newObj)
	if err != nil {
		return obj, err
	}
	obj, ok := decodeObj.(T1)
	if !ok {
		return obj, fmt.Errorf("unexpected object, got: %s", reflect.TypeOf(decodeObj).Name())
	}
	return obj, nil
}

func convert(obj any) (runtime.Object, error) {
	runtimeObj, ok := obj.(runtime.Object)
	if !ok {
		return nil, fmt.Errorf("Unsupported type: %v", reflect.TypeOf(obj))
	}
	return runtimeObj, nil
}

func (r *file[T1]) writeFile(ctx context.Context, key store.Key, obj T1) error {
	log := log.FromContext(ctx)

	runtimeObj, err := convert(obj)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	if err := r.codec.Encode(runtimeObj, buf); err != nil {
		return err
	}
	log.Info("write file", "fileName", r.filename(key), "data", string(buf.Bytes()))
	if err := ensureDir(filepath.Dir(r.filename(key))); err != nil {
		return err
	}
	return os.WriteFile(r.filename(key), buf.Bytes(), 0644)
}

func (r *file[T1]) deleteFile(ctx context.Context, key store.Key) error {
	return os.Remove(r.filename(key))
}

func (r *file[T1]) visitDir(ctx context.Context, visitorFunc func(ctx context.Context, key store.Key, obj T1)) error {
	return filepath.Walk(r.objRootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// skip any non json file
		if !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		// this is a json file by now
		// next step is find the key (namespace and name)
		fmt.Println("path", path)
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		fmt.Println("name", name)
		namespace := ""
		pathSplit := strings.Split(path, "/")
		if len(pathSplit) > (len(strings.Split(r.objRootPath, "/")) + 1) {
			namespace = pathSplit[len(pathSplit)-2]
		}
		fmt.Println("namespace", namespace)
		key := store.KeyFromNSN(types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		})

		newObj, err := r.readFile(ctx, key)
		if err != nil {
			return err
		}
		if visitorFunc != nil {
			visitorFunc(ctx, key, newObj)
		}

		return nil
	})
}

func exists(filepath string) bool {
	_, err := os.Stat(filepath)
	return err == nil
}

func ensureDir(dirname string) error {
	if !exists(dirname) {
		return os.MkdirAll(dirname, 0755)
	}
	return nil
}
