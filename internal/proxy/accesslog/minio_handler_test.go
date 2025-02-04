// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package accesslog

import (
	"context"
	"os"
	"testing"

	"github.com/milvus-io/milvus/internal/util/paramtable"
	"github.com/stretchr/testify/assert"
)

func TestMinioHandler_ConnectError(t *testing.T) {
	var Params paramtable.ComponentParam
	Params.Init()
	testPath := "/tme/miniotest"
	Params.ProxyCfg.AccessLog.LocalPath = testPath
	Params.MinioCfg.UseIAM = true
	Params.MinioCfg.Address = ""
	defer os.RemoveAll(testPath)

	_, err := NewMinioHandler(
		context.Background(),
		&Params.MinioCfg,
		Params.ProxyCfg.AccessLog.RemotePath,
		Params.ProxyCfg.AccessLog.MaxBackups,
	)
	assert.Error(t, err)
}

func TestMinioHandler_Join(t *testing.T) {
	assert.Equal(t, "a/b", Join("a", "b"))
	assert.Equal(t, "a/b", Join("a/", "b"))
}
