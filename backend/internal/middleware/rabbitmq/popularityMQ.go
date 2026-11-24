package rabbitmq

import (
	"context"
	"errors"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type PopularityMQ struct {
	ch *amqp.Channel
}

const (
	popularityExchange   = "video.popularity.events" //交换机名称
	popularityQueue      = "video.popularity.events" //队列名称
	popularityBindingKey = "video.popularity.*"      //binding key:队列与交换机绑定，*表示匹配一个单词

	popularityUpdateRK = "video.popularity.update" //routing key:生产者发送消息时附带的路由标识
)

type PopularityEvent struct {
	//事件的唯一标识，若消费者已经处理了某条消息，但是向 RabbitMQ 确认消息之前，消费者突然宕机，RabbitMQ 可能会重新投递这条消息，这样，同一条消息就可能被处理两次。因此，可以使用 EventID 实现消息去重。
	EventID string `json:"event_id"`

	VideoID    uint      `json:"video_id"`
	Change     int64     `json:"change"`
	OccurredAt time.Time `json:"occurred_at"`
}

// 初始化热度交换机、热度队列、死信、绑定
func NewPopularityMQ(base *RabbitMQ) (*PopularityMQ, error) {
	if base == nil {
		return nil, errors.New("rabbitmq base is nil")
	}
	ch, err := base.NewChannel()
	if err != nil {
		return nil, err
	}
	if err := DeclareTopic(ch, popularityExchange, popularityQueue, popularityBindingKey); err != nil {
		ch.Close()
		return nil, err
	}
	return &PopularityMQ{ch: ch}, nil
}

// 发布消息
func (p *PopularityMQ) Update(ctx context.Context, videoID uint, change int64) error {
	if p == nil || p.ch == nil {
		return errors.New("popularity mq is not initialized")
	}
	if videoID == 0 || change == 0 {
		return errors.New("videoID and change are required")
	}
	id, err := newEventID(16)
	if err != nil {
		return err
	}
	event := PopularityEvent{
		EventID:    id,
		VideoID:    videoID,
		Change:     change,
		OccurredAt: time.Now().UTC(),
	}
	return PublishJSON(ctx, p.ch, popularityExchange, popularityUpdateRK, event)
}
