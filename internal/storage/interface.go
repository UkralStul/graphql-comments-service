package storage

import (
	"context"
	"github.com/UkralStul/graphql-comments-service/internal/domain"
)

// PaginationArgs - аргументы для пагинации.
type PaginationArgs struct {
	Limit  int
	Cursor *string
}

// Storage определяет контракт для хранилищ.
type Storage interface {
	GetPosts(ctx context.Context, limit, offset int) ([]*domain.Post, error)
	GetPostByID(ctx context.Context, id string) (*domain.Post, error)
	CreatePost(ctx context.Context, post *domain.Post) (*domain.Post, error)
	ToggleComments(ctx context.Context, postID string, enable bool) (*domain.Post, error)

	CreateComment(ctx context.Context, comment *domain.Comment) (*domain.Comment, error)
	GetCommentByID(ctx context.Context, id string) (*domain.Comment, error)

	// Методы для пагинации
	GetCommentsByPostID(ctx context.Context, postID string, args PaginationArgs) ([]*domain.Comment, error)
	GetCommentsByParentID(ctx context.Context, parentID string, args PaginationArgs) ([]*domain.Comment, error)

	// Методы для Dataloader'ов
	GetCommentsByParentIDs(ctx context.Context, parentIDs []string) (map[string][]*domain.Comment, error)
}
