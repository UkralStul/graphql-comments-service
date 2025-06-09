package inmemory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/UkralStul/graphql-comments-service/internal/domain"
	"github.com/UkralStul/graphql-comments-service/internal/storage"
	"github.com/google/uuid"
)

// Store реализует интерфейс Storage в памяти.
type Store struct {
	mu               sync.RWMutex
	posts            map[string]*domain.Post
	comments         map[string]*domain.Comment
	commentsByPost   map[string][]string // map[postID][]commentID (только корневые)
	commentsByParent map[string][]string // map[parentID][]commentID
}

// New создает новый экземпляр in-memory хранилища.
func New() *Store {
	return &Store{
		posts:            make(map[string]*domain.Post),
		comments:         make(map[string]*domain.Comment),
		commentsByPost:   make(map[string][]string),
		commentsByParent: make(map[string][]string),
	}
}

// === Post Methods ===

func (s *Store) CreatePost(ctx context.Context, post *domain.Post) (*domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	post.ID = uuid.NewString()
	post.CreatedAt = time.Now().UTC()
	s.posts[post.ID] = post
	return post, nil
}

func (s *Store) GetPostByID(ctx context.Context, id string) (*domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	post, ok := s.posts[id]
	if !ok {
		return nil, fmt.Errorf("post with id %s not found", id)
	}
	return post, nil
}

func (s *Store) GetPosts(ctx context.Context, limit, offset int) ([]*domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allPosts := make([]*domain.Post, 0, len(s.posts))
	for _, p := range s.posts {
		allPosts = append(allPosts, p)
	}

	sort.Slice(allPosts, func(i, j int) bool {
		return allPosts[i].CreatedAt.After(allPosts[j].CreatedAt)
	})

	start := offset
	if start >= len(allPosts) {
		return []*domain.Post{}, nil
	}
	end := start + limit
	if end > len(allPosts) {
		end = len(allPosts)
	}
	return allPosts[start:end], nil
}

func (s *Store) ToggleComments(ctx context.Context, postID string, enable bool) (*domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	post, ok := s.posts[postID]
	if !ok {
		return nil, fmt.Errorf("post with id %s not found", postID)
	}
	post.CommentsEnabled = enable
	return post, nil
}

// === Comment Methods ===

func (s *Store) CreateComment(ctx context.Context, comment *domain.Comment) (*domain.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверка поста
	post, ok := s.posts[comment.PostID]
	if !ok {
		return nil, errors.New("post not found")
	}
	if !post.CommentsEnabled {
		return nil, errors.New("comments are disabled for this post")
	}

	// Проверка длины комментария
	if len(comment.Content) > 2000 {
		return nil, errors.New("comment content is too long")
	}
	if strings.TrimSpace(comment.Content) == "" {
		return nil, errors.New("comment content cannot be empty")
	}

	// Проверка родительского комментария
	if comment.ParentID != nil {
		if _, ok := s.comments[*comment.ParentID]; !ok {
			return nil, errors.New("parent comment not found")
		}
	}

	comment.ID = uuid.NewString()
	comment.CreatedAt = time.Now().UTC()
	s.comments[comment.ID] = comment

	// Обновление индексов для иерархии
	if comment.ParentID == nil {
		// Корневой комментарий
		s.commentsByPost[comment.PostID] = append(s.commentsByPost[comment.PostID], comment.ID)
	} else {
		// Дочерний комментарий
		s.commentsByParent[*comment.ParentID] = append(s.commentsByParent[*comment.ParentID], comment.ID)
	}

	return comment, nil
}

func (s *Store) GetCommentByID(ctx context.Context, id string) (*domain.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	comment, ok := s.comments[id]
	if !ok {
		return nil, errors.New("comment not found")
	}
	return comment, nil
}

// === Pagination Methods ===

func (s *Store) GetCommentsByPostID(ctx context.Context, postID string, args storage.PaginationArgs) ([]*domain.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	commentIDs, ok := s.commentsByPost[postID]
	if !ok {
		return []*domain.Comment{}, nil
	}

	return s.paginateComments(commentIDs, args), nil
}

func (s *Store) GetCommentsByParentID(ctx context.Context, parentID string, args storage.PaginationArgs) ([]*domain.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	commentIDs, ok := s.commentsByParent[parentID]
	if !ok {
		return []*domain.Comment{}, nil
	}

	return s.paginateComments(commentIDs, args), nil
}

// paginateComments - вспомогательная функция для пагинации
func (s *Store) paginateComments(ids []string, args storage.PaginationArgs) []*domain.Comment {
	allComments := make([]*domain.Comment, 0, len(ids))
	for _, id := range ids {
		if c, ok := s.comments[id]; ok {
			allComments = append(allComments, c)
		}
	}
	// Сортируем по времени создания, чтобы пагинация была консистентной
	sort.Slice(allComments, func(i, j int) bool {
		return allComments[i].CreatedAt.Before(allComments[j].CreatedAt)
	})

	startIndex := 0
	if args.Cursor != nil {
		for i, c := range allComments {
			if c.ID == *args.Cursor {
				startIndex = i + 1
				break
			}
		}
	}

	if startIndex >= len(allComments) {
		return []*domain.Comment{}
	}

	endIndex := startIndex + args.Limit
	if endIndex > len(allComments) {
		endIndex = len(allComments)
	}

	return allComments[startIndex:endIndex]
}

// === Dataloader Methods ===

func (s *Store) GetCommentsByParentIDs(ctx context.Context, parentIDs []string) (map[string][]*domain.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make(map[string][]*domain.Comment, len(parentIDs))

	for _, pID := range parentIDs {
		childIDs := s.commentsByParent[pID]
		children := make([]*domain.Comment, 0, len(childIDs))
		for _, cID := range childIDs {
			if c, ok := s.comments[cID]; ok {
				children = append(children, c)
			}
		}
		// Важно: Dataloader'у нужны отсортированные данные для консистентности
		sort.Slice(children, func(i, j int) bool {
			return children[i].CreatedAt.Before(children[j].CreatedAt)
		})
		results[pID] = children
	}

	return results, nil
}
