// 消费者修改数据库，更新like表、video的like_count和popularity
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/jttt-17/feedsystem/backend/internal/middleware/rabbitmq"
	"github.com/jttt-17/feedsystem/backend/internal/video"
	amqp "github.com/rabbitmq/amqp091-go"
)

type LikeWorker struct {
	ch     *amqp.Channel
	likes  *video.LikeRepository
	videos *video.VideoRepository
	queue  string
}

func NewLikeWorker(ch *amqp.Channel, likes *video.LikeRepository, videos *video.VideoRepository, queue string) *LikeWorker {
	return &LikeWorker{ch: ch, likes: likes, videos: videos, queue: queue}
}

func (w *LikeWorker) Run(ctx context.Context) error {
	if w == nil || w.ch == nil || w.likes == nil || w.videos == nil {
		return errors.New("like worker is not initialized")
	}
	if w.queue == "" {
		return errors.New("queue is required")
	}

	deliveries, err := w.ch.Consume(
		w.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	//监听队列
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries channel closed")
			}
			//顺序处理
			w.handleDelivery(ctx, d)
		}
	}

}

func (w *LikeWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
	const maxRetries = 3
	for i := 0; i <= maxRetries; i++ {
		select {
		case <-ctx.Done():
			//消费者被关闭，不代表消息本身有问题，所以重新放回队列
			_ = d.Nack(false, true)
			return
		default:
		}
		if err := w.process(ctx, d.Body); err != nil {
			if i >= maxRetries {
				log.Printf("like worker: 重试 %d 次后仍失败, 丢弃: %v", maxRetries, err)
				_ = d.Nack(false, false)
				return
			}
			//指数退避重试，1s->2s->4s
			wait := time.Duration(1<<uint(i)) * time.Second
			log.Printf("like worker: 处理失败, %v 后重试 (%d/%d): %v", wait, i+1, maxRetries, err)
			time.Sleep(wait)
			continue
		}
		_ = d.Ack(false)
		return
	}

}

func (w *LikeWorker) process(ctx context.Context, body []byte) error {
	var evt rabbitmq.LikeEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		log.Printf("解析事件失败")
		return nil
	}
	if evt.UserID == 0 || evt.VideoID == 0 {
		log.Printf("非法userID或videoID")
		return nil
	}
	switch evt.Action {
	case "like":
		return w.applyLike(ctx, evt.UserID, evt.VideoID)
	case "unlike":
		return w.applyUnlike(ctx, evt.UserID, evt.VideoID)
	default:
		return nil
	}
}

// applyLike和applyUnlike都存在数据一致性的问题
// 若like表更新完出故障了，video还未更新，重试机制第二次尝试修改时，发现like表已经有相应数据了，就不会执行后面的video的修改
// 修改方法：放进同一个 MySQL 事务中
func (w *LikeWorker) applyLike(ctx context.Context, userID, videoID uint) error {
	ok, err := w.videos.IsExist(ctx, videoID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	//更新like表
	created, err := w.likes.LikeIgnoreDuplicate(ctx, &video.Like{
		VideoID:   videoID,
		AccountID: userID,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	//更新video的like_count
	if err := w.videos.ChangeLikeCount(ctx, videoID, 1); err != nil {
		return err
	}
	//更新video的popularity
	return w.videos.ChangePopularity(ctx, videoID, 1)
}

func (w *LikeWorker) applyUnlike(ctx context.Context, userID, videoID uint) error {
	ok, err := w.videos.IsExist(ctx, videoID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	deleted, err := w.likes.DeleteByVideoAndAccount(ctx, videoID, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return nil
	}

	if err := w.videos.ChangeLikeCount(ctx, videoID, -1); err != nil {
		return err
	}
	return w.videos.ChangePopularity(ctx, videoID, -1)
}
