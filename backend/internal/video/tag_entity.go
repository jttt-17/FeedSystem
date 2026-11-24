package video

import "regexp"

//Tag表，话题标签唯一
type Tag struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"uniqueIndex;type:varchar(100);not null" json:"name"`
}

//videoID和TagID多对多关系
type VideoTag struct {
	ID      uint `gorm:"primaryKey"`
	VideoID uint `gorm:"index;not null"`
	TagID   uint `gorm:"index;not null"`
}

//正则匹配：#
var tagRegex = regexp.MustCompile(`#([\p{L}\p{N}_]+)`)

func ExtractTags(text string) []string {
	//返回比如:
	// 	matches := [][]string{
	//     {"#Go语言", "Go语言"},
	//     {"#编程", "编程"},
	//     {"#学习", "学习"},
	// }
	matches := tagRegex.FindAllStringSubmatch(text, -1)

	//{"Go语言":true,"编程":true}
	seen := make(map[string]bool)

	var tags []string
	for _, m := range matches {
		tag := m[1]
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}
