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

package checkers

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	etcdkv "github.com/milvus-io/milvus/internal/kv/etcd"
	"github.com/milvus-io/milvus/internal/proto/datapb"
	"github.com/milvus-io/milvus/internal/proto/internalpb"
	"github.com/milvus-io/milvus/internal/querycoordv2/balance"
	"github.com/milvus-io/milvus/internal/querycoordv2/meta"
	. "github.com/milvus-io/milvus/internal/querycoordv2/params"
	"github.com/milvus-io/milvus/internal/querycoordv2/task"
	"github.com/milvus-io/milvus/internal/querycoordv2/utils"
	"github.com/milvus-io/milvus/internal/util/etcd"
)

type SegmentCheckerTestSuite struct {
	suite.Suite
	kv      *etcdkv.EtcdKV
	checker *SegmentChecker
	meta    *meta.Meta
	broker  *meta.MockBroker
}

func (suite *SegmentCheckerTestSuite) SetupSuite() {
	Params.Init()
}

func (suite *SegmentCheckerTestSuite) SetupTest() {
	var err error
	config := GenerateEtcdConfig()
	cli, err := etcd.GetEtcdClient(&config)
	suite.Require().NoError(err)
	suite.kv = etcdkv.NewEtcdKV(cli, config.MetaRootPath)

	// meta
	store := meta.NewMetaStore(suite.kv)
	idAllocator := RandomIncrementIDAllocator()
	suite.meta = meta.NewMeta(idAllocator, store)
	distManager := meta.NewDistributionManager()
	suite.broker = meta.NewMockBroker(suite.T())
	targetManager := meta.NewTargetManager(suite.broker, suite.meta)

	balancer := suite.createMockBalancer()
	suite.checker = NewSegmentChecker(suite.meta, distManager, targetManager, balancer)
}

func (suite *SegmentCheckerTestSuite) TearDownTest() {
	suite.kv.Close()
}

func (suite *SegmentCheckerTestSuite) createMockBalancer() balance.Balance {
	balancer := balance.NewMockBalancer(suite.T())
	balancer.EXPECT().AssignSegment(mock.Anything, mock.Anything).Maybe().Return(func(segments []*meta.Segment, nodes []int64) []balance.SegmentAssignPlan {
		plans := make([]balance.SegmentAssignPlan, 0, len(segments))
		for i, s := range segments {
			plan := balance.SegmentAssignPlan{
				Segment:   s,
				From:      -1,
				To:        nodes[i%len(nodes)],
				ReplicaID: -1,
			}
			plans = append(plans, plan)
		}
		return plans
	})
	return balancer
}

func (suite *SegmentCheckerTestSuite) TestLoadSegments() {
	checker := suite.checker
	// set meta
	checker.meta.CollectionManager.PutCollection(utils.CreateTestCollection(1, 1))
	checker.meta.ReplicaManager.Put(utils.CreateTestReplica(1, 1, []int64{1, 2}))

	// set target
	segments := []*datapb.SegmentBinlogs{
		{
			SegmentID:     1,
			InsertChannel: "test-insert-channel",
		},
	}
	suite.broker.EXPECT().GetRecoveryInfo(mock.Anything, int64(1), int64(1)).Return(
		nil, segments, nil)
	checker.targetMgr.UpdateCollectionNextTargetWithPartitions(int64(1), int64(1))

	// set dist
	checker.dist.ChannelDistManager.Update(2, utils.CreateTestChannel(1, 2, 1, "test-insert-channel"))
	checker.dist.LeaderViewManager.Update(2, utils.CreateTestLeaderView(2, 1, "test-insert-channel", map[int64]int64{}, map[int64]*meta.Segment{}))

	tasks := checker.Check(context.TODO())
	suite.Len(tasks, 1)
	suite.Len(tasks[0].Actions(), 1)
	action, ok := tasks[0].Actions()[0].(*task.SegmentAction)
	suite.True(ok)
	suite.EqualValues(1, tasks[0].ReplicaID())
	suite.Equal(task.ActionTypeGrow, action.Type())
	suite.EqualValues(1, action.SegmentID())
	suite.Equal(tasks[0].Priority(), task.TaskPriorityNormal)

}

func (suite *SegmentCheckerTestSuite) TestReleaseSegments() {
	checker := suite.checker
	// set meta
	checker.meta.CollectionManager.PutCollection(utils.CreateTestCollection(1, 1))
	checker.meta.ReplicaManager.Put(utils.CreateTestReplica(1, 1, []int64{1, 2}))

	// set dist
	checker.dist.ChannelDistManager.Update(2, utils.CreateTestChannel(1, 2, 1, "test-insert-channel"))
	checker.dist.LeaderViewManager.Update(2, utils.CreateTestLeaderView(2, 1, "test-insert-channel", map[int64]int64{}, map[int64]*meta.Segment{}))
	checker.dist.SegmentDistManager.Update(1, utils.CreateTestSegment(1, 1, 2, 1, 1, "test-insert-channel"))

	tasks := checker.Check(context.TODO())
	suite.Len(tasks, 1)
	suite.Len(tasks[0].Actions(), 1)
	action, ok := tasks[0].Actions()[0].(*task.SegmentAction)
	suite.True(ok)
	suite.EqualValues(1, tasks[0].ReplicaID())
	suite.Equal(task.ActionTypeReduce, action.Type())
	suite.EqualValues(2, action.SegmentID())
	suite.Equal(tasks[0].Priority(), task.TaskPriorityNormal)
}

func (suite *SegmentCheckerTestSuite) TestReleaseRepeatedSegments() {
	checker := suite.checker
	// set meta
	checker.meta.CollectionManager.PutCollection(utils.CreateTestCollection(1, 1))
	checker.meta.ReplicaManager.Put(utils.CreateTestReplica(1, 1, []int64{1, 2}))

	// set target
	segments := []*datapb.SegmentBinlogs{
		{
			SegmentID:     1,
			InsertChannel: "test-insert-channel",
		},
	}
	suite.broker.EXPECT().GetRecoveryInfo(mock.Anything, int64(1), int64(1)).Return(
		nil, segments, nil)
	checker.targetMgr.UpdateCollectionNextTargetWithPartitions(int64(1), int64(1))

	// set dist
	checker.dist.ChannelDistManager.Update(2, utils.CreateTestChannel(1, 2, 1, "test-insert-channel"))
	checker.dist.LeaderViewManager.Update(2, utils.CreateTestLeaderView(2, 1, "test-insert-channel", map[int64]int64{1: 2}, map[int64]*meta.Segment{}))
	checker.dist.SegmentDistManager.Update(1, utils.CreateTestSegment(1, 1, 1, 1, 1, "test-insert-channel"))
	checker.dist.SegmentDistManager.Update(2, utils.CreateTestSegment(1, 1, 1, 1, 2, "test-insert-channel"))

	tasks := checker.Check(context.TODO())
	suite.Len(tasks, 1)
	suite.Len(tasks[0].Actions(), 1)
	action, ok := tasks[0].Actions()[0].(*task.SegmentAction)
	suite.True(ok)
	suite.EqualValues(1, tasks[0].ReplicaID())
	suite.Equal(task.ActionTypeReduce, action.Type())
	suite.EqualValues(1, action.SegmentID())
	suite.EqualValues(1, action.Node())
	suite.Equal(tasks[0].Priority(), task.TaskPriorityNormal)

	// test less version exist on leader
	checker.dist.LeaderViewManager.Update(2, utils.CreateTestLeaderView(2, 1, "test-insert-channel", map[int64]int64{1: 1}, map[int64]*meta.Segment{}))
	tasks = checker.Check(context.TODO())
	suite.Len(tasks, 0)
}

func (suite *SegmentCheckerTestSuite) TestReleaseGrowingSegments() {
	checker := suite.checker
	// segment3 is compacted from segment2, and node2 has growing segments 2 and 3. checker should generate
	// 2 tasks to reduce segment 2 and 3.
	checker.meta.CollectionManager.PutCollection(utils.CreateTestCollection(1, 1))
	checker.meta.ReplicaManager.Put(utils.CreateTestReplica(1, 1, []int64{1, 2}))

	segments := []*datapb.SegmentBinlogs{
		{
			SegmentID:     3,
			InsertChannel: "test-insert-channel",
		},
	}
	channels := []*datapb.VchannelInfo{
		{
			CollectionID: 1,
			ChannelName:  "test-insert-channel",
			SeekPosition: &internalpb.MsgPosition{Timestamp: 10},
		},
	}
	suite.broker.EXPECT().GetRecoveryInfo(mock.Anything, int64(1), int64(1)).Return(
		channels, segments, nil)
	checker.targetMgr.UpdateCollectionNextTargetWithPartitions(int64(1), int64(1))
	checker.targetMgr.UpdateCollectionCurrentTarget(int64(1), int64(1))

	growingSegments := make(map[int64]*meta.Segment)
	growingSegments[2] = utils.CreateTestSegment(1, 1, 2, 2, 0, "test-insert-channel")
	growingSegments[2].SegmentInfo.StartPosition = &internalpb.MsgPosition{Timestamp: 2}
	growingSegments[3] = utils.CreateTestSegment(1, 1, 3, 2, 1, "test-insert-channel")
	growingSegments[3].SegmentInfo.StartPosition = &internalpb.MsgPosition{Timestamp: 3}
	growingSegments[4] = utils.CreateTestSegment(1, 1, 4, 2, 1, "test-insert-channel")
	growingSegments[4].SegmentInfo.StartPosition = &internalpb.MsgPosition{Timestamp: 11}

	dmChannel := utils.CreateTestChannel(1, 2, 1, "test-insert-channel")
	dmChannel.UnflushedSegmentIds = []int64{2, 3}
	checker.dist.ChannelDistManager.Update(2, dmChannel)
	checker.dist.LeaderViewManager.Update(2, utils.CreateTestLeaderView(2, 1, "test-insert-channel", map[int64]int64{3: 2}, growingSegments))
	checker.dist.SegmentDistManager.Update(2, utils.CreateTestSegment(1, 1, 3, 2, 2, "test-insert-channel"))

	tasks := checker.Check(context.TODO())
	suite.Len(tasks, 2)
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Actions()[0].(*task.SegmentAction).SegmentID() < tasks[j].Actions()[0].(*task.SegmentAction).SegmentID()
	})
	suite.Len(tasks[0].Actions(), 1)
	action, ok := tasks[0].Actions()[0].(*task.SegmentAction)
	suite.True(ok)
	suite.EqualValues(1, tasks[0].ReplicaID())
	suite.Equal(task.ActionTypeReduce, action.Type())
	suite.EqualValues(2, action.SegmentID())
	suite.EqualValues(2, action.Node())
	suite.Equal(tasks[0].Priority(), task.TaskPriorityNormal)

	suite.Len(tasks[1].Actions(), 1)
	action, ok = tasks[1].Actions()[0].(*task.SegmentAction)
	suite.True(ok)
	suite.EqualValues(1, tasks[1].ReplicaID())
	suite.Equal(task.ActionTypeReduce, action.Type())
	suite.EqualValues(3, action.SegmentID())
	suite.EqualValues(2, action.Node())
	suite.Equal(tasks[1].Priority(), task.TaskPriorityNormal)
}

func (suite *SegmentCheckerTestSuite) TestReleaseDroppedSegments() {
	checker := suite.checker
	checker.dist.SegmentDistManager.Update(1, utils.CreateTestSegment(1, 1, 1, 1, 1, "test-insert-channel"))
	tasks := checker.Check(context.TODO())
	suite.Len(tasks, 1)
	suite.Len(tasks[0].Actions(), 1)
	action, ok := tasks[0].Actions()[0].(*task.SegmentAction)
	suite.True(ok)
	suite.EqualValues(-1, tasks[0].ReplicaID())
	suite.Equal(task.ActionTypeReduce, action.Type())
	suite.EqualValues(1, action.SegmentID())
	suite.EqualValues(1, action.Node())
	suite.Equal(tasks[0].Priority(), task.TaskPriorityNormal)
}

func TestSegmentCheckerSuite(t *testing.T) {
	suite.Run(t, new(SegmentCheckerTestSuite))
}
