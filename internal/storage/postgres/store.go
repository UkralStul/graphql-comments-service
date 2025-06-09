package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/UkralStul/graphql-comments-service/internal/domain"
	"github.com/UkralStul/graphql-comments-service/internal/storage"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store реализует интерфейс Storage с использованием PostgreSQL.
type Store struct {
	db *gorm.DB
}

// New создает новый экземпляр хранилища PostgreSQL.
func New(dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // Включаем логирование для отладки
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Выполняем миграцию схемы
	if err := db.AutoMigrate(&domain.Post{}, &domain.Comment{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &Store{db: db}, nil
}

// === Post Methods ===

func (s *Store) CreatePost(ctx context.Context, post *domain.Post) (*domain.Post, error) {
	if err := s.db.WithContext(ctx).Create(post).Error; err != nil {
		return nil, err
	}
	// GORM автоматически заполнит ID и CreatedAt после создания
	return post, nil
}

func (s *Store) GetPostByID(ctx context.Context, id string) (*domain.Post, error) {
	var post domain.Post
	if err := s.db.WithContext(ctx).First(&post, "id = ?", id).Error; err != nil {
		// GORM возвращает gorm.ErrRecordNotFound, если запись не найдена
		return nil, err
	}
	return &post, nil
}

func (s *Store) GetCommentByID(ctx context.Context, id string) (*domain.Comment, error) {
	var comment domain.Comment
	if err := s.db.WithContext(ctx).First(&comment, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &comment, nil
}

func (s *Store) GetPosts(ctx context.Context, limit, offset int) ([]*domain.Post, error) {
	var posts []*domain.Post
	err := s.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Offset(offset).Find(&posts).Error
	return posts, err
}

func (s *Store) ToggleComments(ctx context.Context, postID string, enable bool) (*domain.Post, error) {
	var post domain.Post
	// Используем транзакцию для атомарности операции чтения-записи
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&post, "id = ?", postID).Error; err != nil {
			return err
		}
		post.CommentsEnabled = enable
		if err := tx.Save(&post).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &post, nil
}

// === Comment Methods ===

func (s *Store) CreateComment(ctx context.Context, comment *domain.Comment) (*domain.Comment, error) {
	// Валидация
	if len(comment.Content) > 2000 {
		return nil, errors.New("comment content is too long")
	}
	if strings.TrimSpace(comment.Content) == "" {
		return nil, errors.New("comment content cannot be empty")
	}

	// Проверяем существование поста и разрешение на комментирование в одной транзакции
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var post domain.Post
		if err := tx.Select("comments_enabled").First(&post, "id = ?", comment.PostID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("post not found")
			}
			return err
		}
		if !post.CommentsEnabled {
			return errors.New("comments are disabled for this post")
		}

		// Если есть родитель, проверяем его существование
		if comment.ParentID != nil {
			var parentCommentCount int64
			if err := tx.Model(&domain.Comment{}).Where("id = ?", *comment.ParentID).Count(&parentCommentCount).Error; err != nil {
				return err
			}
			if parentCommentCount == 0 {
				return errors.New("parent comment not found")
			}
		}

		// Создаем комментарий
		if err := tx.Create(comment).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return comment, nil
}

// === Pagination Methods ===

func (s *Store) GetCommentsByPostID(ctx context.Context, postID string, args storage.PaginationArgs) ([]*domain.Comment, error) {
	var comments []*domain.Comment
	// Выбираем только комментарии верхнего уровня для поста (parent_id IS NULL)
	query := s.db.WithContext(ctx).
		Where("post_id = ? AND parent_id IS NULL", postID).
		Order("created_at ASC").
		Limit(args.Limit)

	// Реализация курсорной пагинации
	if args.Cursor != nil {
		var cursorComment domain.Comment
		// Находим время создания комментария-курсора
		if err := s.db.First(&cursorComment, "id = ?", *args.Cursor).Error; err == nil {
			// И выбираем все записи, созданные ПОСЛЕ него
			query = query.Where("created_at > ?", cursorComment.CreatedAt)
		}
	}

	err := query.Find(&comments).Error
	return comments, err
}

func (s *Store) GetCommentsByParentID(ctx context.Context, parentID string, args storage.PaginationArgs) ([]*domain.Comment, error) {
	var comments []*domain.Comment
	// Аналогично, но для дочерних комментариев
	query := s.db.WithContext(ctx).
		Where("parent_id = ?", parentID).
		Order("created_at ASC").
		Limit(args.Limit)

	if args.Cursor != nil {
		var cursorComment domain.Comment
		if err := s.db.First(&cursorComment, "id = ?", *args.Cursor).Error; err == nil {
			query = query.Where("created_at > ?", cursorComment.CreatedAt)
		}
	}

	err := query.Find(&comments).Error
	return comments, err
}

// === Dataloader Method ===

func (s *Store) GetCommentsByParentIDs(ctx context.Context, parentIDs []string) (map[string][]*domain.Comment, error) {
	var comments []*domain.Comment
	// Загружаем все дочерние комментарии для всех переданных parentID одним запросом
	err := s.db.WithContext(ctx).
		Where("parent_id IN ?", parentIDs).
		Order("parent_id, created_at ASC"). // Сортируем для правильной группировки и порядка
		Find(&comments).Error

	if err != nil {
		return nil, err
	}

	// Группируем результаты в карту map[parentID][]*Comment
	result := make(map[string][]*domain.Comment, len(parentIDs))
	for _, c := range comments {
		if c.ParentID != nil {
			result[*c.ParentID] = append(result[*c.ParentID], c)
		}
	}

	return result, nil
}
