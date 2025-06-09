// graph/resolver.go

package graph

import (
	"github.com/UkralStul/graphql-comments-service/internal/domain"
	"github.com/UkralStul/graphql-comments-service/internal/storage"
	"sync"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

// CommentObserver хранит каналы для подписчиков на комментарии.
type CommentObserver struct {
	mu sync.RWMutex
	//          map[postID] map[subscriberID] channel
	subs map[string]map[string]chan *domain.Comment
}

// NewCommentObserver - конструктор для нашего наблюдателя.
func NewCommentObserver() *CommentObserver {
	return &CommentObserver{
		subs: make(map[string]map[string]chan *domain.Comment),
	}
}

// Resolver - это корневая структура резолвера.
// Она содержит все зависимости, которые нужны для выполнения запросов.
type Resolver struct {
	Storage  storage.Storage
	Observer *CommentObserver
}
