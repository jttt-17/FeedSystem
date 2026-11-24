package video

import "time"

//表likes记录：谁对哪个视频在何时点了个赞
type Like struct {
	ID uint `gorm:"primaryKey" json:"id"`

	//uniqueIndex:idx_like_video_account
	//VideoID和AccountID建立联合唯一索引

	VideoID   uint      `gorm:"uniqueIndex:idx_like_video_account;not null" json:"video_id"`
	AccountID uint      `gorm:"uniqueIndex:idx_like_video_account;not null" json:"account_id"`
	CreatedAt time.Time `json:"created_at"`
}

type LikeRequest struct {
	VideoID uint `json:"video_id"`
}
