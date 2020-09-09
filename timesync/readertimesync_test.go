package readertimesync

import (
	"context"
	"github.com/apache/pulsar-client-go/pulsar"
	pb "github.com/czs007/suvlim/pkg/message"
	"github.com/golang/protobuf/proto"
	"log"
	"testing"
	"time"
)

const (
	pulsarAddr      = "pulsar://localhost:6650"
	timeSyncTopic   = "timesync"
	timeSyncSubName = "timesync-g"
	readerTopic1    = "reader1"
	readerTopic2    = "reader2"
	readerTopic3    = "reader3"
	readerTopic4    = "reader4"
	readerSubName   = "reader-g"
	interval        = 200
)

func TestAlignTimeSync(t *testing.T) {
	r := &readerTimeSyncCfg{
		proxyIdList: []int64{1, 2, 3},
		interval:    200,
	}
	ts := []*pb.TimeSyncMsg{
		{
			Peer_Id:   1,
			Timestamp: 5 << 18,
		},
		{
			Peer_Id:   3,
			Timestamp: 15 << 18,
		},
		{
			Peer_Id:   2,
			Timestamp: 20 << 18,
		},
	}
	r.alignTimeSync(ts)
	if len(r.proxyIdList) != 3 {
		t.Fatalf("proxyIdList should be : 1 2 3")
	}
	for i := 0; i < len(r.proxyIdList); i++ {
		if r.proxyIdList[i] != ts[i].Peer_Id {
			t.Fatalf("Align falied")
		}
	}

}

func TestAlignTimeSync2(t *testing.T) {
	r := &readerTimeSyncCfg{
		proxyIdList: []int64{1, 2, 3},
		interval:    200,
	}
	ts := []*pb.TimeSyncMsg{
		{
			Peer_Id:   1,
			Timestamp: 5 << 18,
		},
		{
			Peer_Id:   3,
			Timestamp: 150 << 18,
		},
		{
			Peer_Id:   2,
			Timestamp: 20 << 18,
		},
	}
	ts = r.alignTimeSync(ts)
	if len(r.proxyIdList) != 3 {
		t.Fatalf("proxyIdList should be : 1 2 3")
	}
	if len(ts) != 1 || ts[0].Peer_Id != 2 {
		t.Fatalf("align failed")
	}

}

func TestAlignTimeSync3(t *testing.T) {
	r := &readerTimeSyncCfg{
		proxyIdList: []int64{1, 2, 3},
		interval:    200,
	}
	ts := []*pb.TimeSyncMsg{
		{
			Peer_Id:   1,
			Timestamp: 5 << 18,
		},
		{
			Peer_Id:   1,
			Timestamp: 5 << 18,
		},
		{
			Peer_Id:   1,
			Timestamp: 5 << 18,
		},
		{
			Peer_Id:   3,
			Timestamp: 15 << 18,
		},
		{
			Peer_Id:   2,
			Timestamp: 20 << 18,
		},
	}
	ts = r.alignTimeSync(ts)
	if len(r.proxyIdList) != 3 {
		t.Fatalf("proxyIdList should be : 1 2 3")
	}
	for i := 0; i < len(r.proxyIdList); i++ {
		if r.proxyIdList[i] != ts[i].Peer_Id {
			t.Fatalf("Align falied")
		}
	}
}

func TestNewReaderTimeSync(t *testing.T) {
	r, err := NewReaderTimeSync(pulsarAddr,
		timeSyncTopic,
		timeSyncSubName,
		[]string{readerTopic1, readerTopic2, readerTopic3, readerTopic4},
		readerSubName,
		[]int64{2, 1},
		interval,
		WithReaderQueueSize(8),
	)
	if err != nil {
		t.Fatal(err)
	}
	rr := r.(*readerTimeSyncCfg)
	if rr.pulsarClient == nil {
		t.Fatalf("create pulsar client failed")
	}
	if rr.timeSyncConsumer == nil {
		t.Fatalf("create time sync consumer failed")
	}
	if rr.readerConsumer == nil {
		t.Fatalf("create reader consumer failed")
	}
	if len(rr.readerProducer) != 4 {
		t.Fatalf("create reader producer failed")
	}
	if rr.interval != interval {
		t.Fatalf("interval shoudl be %d", interval)
	}
	if rr.readerQueueSize != 8 {
		t.Fatalf("set read queue size failed")
	}
	if len(rr.proxyIdList) != 2 {
		t.Fatalf("set proxy id failed")
	}
	if rr.proxyIdList[0] != 1 || rr.proxyIdList[1] != 2 {
		t.Fatalf("set proxy id failed")
	}
	r.Close()
}

func TestPulsarClient(t *testing.T) {
	t.Skip("skip pulsar client")
	client, err := pulsar.NewClient(pulsar.ClientOptions{URL: pulsarAddr})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	go startWriteTimeSync(1, timeSyncTopic, client, 2*time.Second, t)
	go startWriteTimeSync(2, timeSyncTopic, client, 2*time.Second, t)
	timeSyncChan := make(chan pulsar.ConsumerMessage)
	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:                       timeSyncTopic,
		SubscriptionName:            timeSyncSubName,
		Type:                        pulsar.KeyShared,
		SubscriptionInitialPosition: pulsar.SubscriptionPositionEarliest,
		MessageChannel:              timeSyncChan,
	})
	if err != nil {
		log.Fatal(err)
	}
	for {
		select {
		case cm := <-timeSyncChan:
			msg := cm.Message
			var tsm pb.TimeSyncMsg
			if err := proto.Unmarshal(msg.Payload(), &tsm); err != nil {
				log.Fatal(err)
			}
			consumer.AckID(msg.ID())
			log.Printf("read time stamp, id = %d, time stamp = %d\n", tsm.Peer_Id, tsm.Timestamp)
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
}

func TestReaderTimesync(t *testing.T) {
	r, err := NewReaderTimeSync(pulsarAddr,
		timeSyncTopic,
		timeSyncSubName,
		[]string{readerTopic1, readerTopic2, readerTopic3, readerTopic4},
		readerSubName,
		[]int64{2, 1},
		interval,
		WithReaderQueueSize(1024),
	)
	if err != nil {
		t.Fatal(err)
	}
	rr := r.(*readerTimeSyncCfg)
	pt1, err := rr.pulsarClient.CreateProducer(pulsar.ProducerOptions{Topic: timeSyncTopic})
	if err != nil {
		t.Fatalf("create time sync producer 1 error %v", err)
	}

	pt2, err := rr.pulsarClient.CreateProducer(pulsar.ProducerOptions{Topic: timeSyncTopic})
	if err != nil {
		t.Fatalf("create time sync producer 2 error %v", err)
	}

	pr1, err := rr.pulsarClient.CreateProducer(pulsar.ProducerOptions{Topic: readerTopic1})
	if err != nil {
		t.Fatalf("create reader 1 error %v", err)
	}

	pr2, err := rr.pulsarClient.CreateProducer(pulsar.ProducerOptions{Topic: readerTopic2})
	if err != nil {
		t.Fatalf("create reader 2 error %v", err)
	}

	pr3, err := rr.pulsarClient.CreateProducer(pulsar.ProducerOptions{Topic: readerTopic3})
	if err != nil {
		t.Fatalf("create reader 3 error %v", err)
	}

	pr4, err := rr.pulsarClient.CreateProducer(pulsar.ProducerOptions{Topic: readerTopic4})
	if err != nil {
		t.Fatalf("create reader 4 error %v", err)
	}

	go startProxy(pt1, 1, pr1, 1, pr2, 2, 2*time.Second, t)
	go startProxy(pt2, 2, pr3, 3, pr4, 4, 2*time.Second, t)

	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	r.Start()

	var tsm1, tsm2 TimeSyncMsg
	for {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			tsm1.NumRecorders = 0
			break
		case tsm1 = <-r.TimeSync():

		}
		if tsm1.NumRecorders > 0 {
			for i := int64(0); i < tsm1.NumRecorders; i++ {
				im := <-r.InsertOrDelete()
				log.Printf("%d - %d", im.Timestamp, tsm2.Timestamp)
				if im.Timestamp < tsm2.Timestamp {
					t.Fatalf("time sync error , im.Timestamp = %d, tsm2.Timestamp = %d", im.Timestamp, tsm2.Timestamp)
				}
			}
			tsm2 = tsm1
		}

	}
	r.Close()
}

func startWriteTimeSync(id int64, topic string, client pulsar.Client, duration time.Duration, t *testing.T) {
	p, _ := client.CreateProducer(pulsar.ProducerOptions{Topic: topic})
	ticker := time.Tick(interval * time.Millisecond)
	numSteps := int(duration / (interval * time.Millisecond))
	var tm uint64 = 0
	for i := 0; i < numSteps; i++ {
		<-ticker
		tm += interval
		tsm := pb.TimeSyncMsg{Timestamp: tm << 18, Peer_Id: id}
		tb, _ := proto.Marshal(&tsm)
		if _, err := p.Send(context.Background(), &pulsar.ProducerMessage{Payload: tb}); err != nil {
			t.Fatalf("send failed tsm id=%d, timestamp=%d, err=%v", tsm.Peer_Id, tsm.Timestamp, err)
		} else {
			//log.Printf("send tsm id=%d, timestamp=%d", tsm.Peer_Id, tsm.Timestamp)
		}
	}
}

func startProxy(pt pulsar.Producer, ptid int64, pr1 pulsar.Producer, prid1 int64, pr2 pulsar.Producer, prid2 int64, duration time.Duration, t *testing.T) {
	total := int(duration / (10 * time.Millisecond))
	ticker := time.Tick(10 * time.Millisecond)
	var timestamp uint64 = 0
	for i := 1; i <= total; i++ {
		<-ticker
		timestamp += 10
		msg := pb.InsertOrDeleteMsg{ClientId: prid1, Timestamp: timestamp << 18}
		mb, err := proto.Marshal(&msg)
		if err != nil {
			t.Fatalf("marshal error %v", err)
		}
		if _, err := pr1.Send(context.Background(), &pulsar.ProducerMessage{Payload: mb}); err != nil {
			t.Fatalf("send msg error %v", err)
		}

		msg.ClientId = prid2
		mb, err = proto.Marshal(&msg)
		if err != nil {
			t.Fatalf("marshal error %v", err)
		}
		if _, err := pr2.Send(context.Background(), &pulsar.ProducerMessage{Payload: mb}); err != nil {
			t.Fatalf("send msg error %v", err)
		}

		//log.Printf("send msg id = [ %d %d ], timestamp = %d", prid1, prid2, timestamp)

		if i%20 == 0 {
			tm := pb.TimeSyncMsg{Peer_Id: ptid, Timestamp: timestamp << 18}
			tb, err := proto.Marshal(&tm)
			if err != nil {
				t.Fatalf("marshal error %v", err)
			}
			if _, err := pt.Send(context.Background(), &pulsar.ProducerMessage{Payload: tb}); err != nil {
				t.Fatalf("send msg error %v", err)
			}
			//log.Printf("send timestamp id = %d, timestamp = %d", ptid, timestamp)
		}
	}
}
