package domain

import "time"

// Post представляет пост в системе.
type Post struct {
	ID              string     `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Title           string     `json:"title" gorm:"type:varchar(255);not null"`
	Content         string     `json:"content" gorm:"type:text;not null"`
	AuthorID        string     `json:"authorId" gorm:"type:varchar(255);not null"`
	CommentsEnabled bool       `json:"commentsEnabled" gorm:"not null;default:true"`
	CreatedAt       time.Time  `json:"createdAt" gorm:"not null;default:now()"`
	Comments        []*Comment `json:"-" gorm:"foreignKey:PostID"` // gorm only
}

// Comment представляет комментарий к посту.
type Comment struct {
	ID        string     `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	PostID    string     `json:"postId" gorm:"type:uuid;not null;index"`
	ParentID  *string    `json:"parentId,omitempty" gorm:"type:uuid;index"`
	AuthorID  string     `json:"authorId" gorm:"type:varchar(255);not null"`
	Content   string     `json:"content" gorm:"type:varchar(2000);not null"`
	CreatedAt time.Time  `json:"createdAt" gorm:"not null;default:now()"`
	Children  []*Comment `json:"-" gorm:"foreignKey:ParentID"` // gorm only
}
