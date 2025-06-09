package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/UkralStul/graphql-comments-service/graph/generated"
	"github.com/UkralStul/graphql-comments-service/graph/model"
	"github.com/UkralStul/graphql-comments-service/internal/domain"
	"github.com/UkralStul/graphql-comments-service/internal/storage"
)

// === Comment Resolvers ===

// Parent резолвер для получения родительского комментария.
// В простом случае, как наш, где вложенность неглубокая, Dataloader не обязателен.
// В продакшене для глубоких деревьев мог бы понадобиться.
func (r *commentResolver) Parent(ctx context.Context, obj *domain.Comment) (*domain.Comment, error) {
	if obj.ParentID == nil {
		return nil, nil
	}
	// Эта реализация вызовет N+1 проблему, если запрашивать родителя для списка комментариев.
	// Для получения одного родителя это приемлемо.
	// Правильное решение - использовать Dataloader, как для Children.
	// panic("not implemented, use Dataloader")
	return r.Storage.GetCommentByID(ctx, *obj.ParentID)
}

// Children резолвер для получения дочерних комментариев.
func (r *commentResolver) Children(ctx context.Context, obj *domain.Comment, limit *int, cursor *string) (*model.CommentConnection, error) {
	// Для этого поля мы НЕ используем Dataloader, т.к. нам нужна пагинация,
	// а Dataloader обычно загружает ВСЕ дочерние элементы.
	// Будем делать прямой запрос к хранилищу.
	l := 5 // Default limit from schema
	if limit != nil {
		l = *limit
	}

	// Запрашиваем на один элемент больше, чтобы определить, есть ли следующая страница
	comments, err := r.Storage.GetCommentsByParentID(ctx, obj.ID, storage.PaginationArgs{Limit: l + 1, Cursor: cursor})
	if err != nil {
		return nil, fmt.Errorf("failed to get children comments: %w", err)
	}

	hasNextPage := len(comments) > l
	if hasNextPage {
		comments = comments[:l] // Убираем лишний элемент
	}

	edges := make([]*model.CommentEdge, len(comments))
	for i, c := range comments {
		edges[i] = &model.CommentEdge{Node: c, Cursor: c.ID}
	}

	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.CommentConnection{
		Edges: edges,
		PageInfo: &model.PageInfo{
			HasNextPage: hasNextPage,
			EndCursor:   endCursor,
		},
	}, nil
}

// === Mutation Resolvers ===

func (r *mutationResolver) CreatePost(ctx context.Context, input model.NewPost) (*domain.Post, error) {
	post := &domain.Post{
		Title:           input.Title,
		Content:         input.Content,
		AuthorID:        input.AuthorID,
		CommentsEnabled: true,
	}
	return r.Storage.CreatePost(ctx, post)
}

func (r *mutationResolver) ToggleComments(ctx context.Context, postID string, enable bool) (*domain.Post, error) {
	// Добавим проверку на существование поста
	_, err := r.Storage.GetPostByID(ctx, postID)
	if err != nil {
		return nil, errors.New("post not found")
	}
	return r.Storage.ToggleComments(ctx, postID, enable)
}

func (r *mutationResolver) CreateComment(ctx context.Context, input model.NewComment) (*domain.Comment, error) {
	comment := &domain.Comment{
		PostID:   input.PostID,
		ParentID: input.ParentID,
		AuthorID: input.AuthorID,
		Content:  input.Content,
	}

	newComment, err := r.Storage.CreateComment(ctx, comment)
	if err != nil {
		return nil, err // Ошибки (пост не найден, комменты выключены) обрабатываются в слое Storage
	}

	// Асинхронно уведомляем подписчиков
	r.Observer.mu.RLock()
	if postSubs, ok := r.Observer.subs[newComment.PostID]; ok {
		// Запускаем в горутине, чтобы не блокировать мутацию
		go func(c *domain.Comment) {
			for _, ch := range postSubs {
				select {
				case ch <- c:
				default:
					// Клиент не успевает читать, можно пропустить или закрыть канал
				}
			}
		}(newComment)
	}
	r.Observer.mu.RUnlock()

	return newComment, nil
}

// === Post Resolvers ===

func (r *postResolver) Comments(ctx context.Context, obj *domain.Post, limit *int, cursor *string) (*model.CommentConnection, error) {
	// Это резолвер для комментариев ВЕРХНЕГО уровня.
	l := 10 // Default limit from schema
	if limit != nil {
		l = *limit
	}

	// Запрашиваем на один элемент больше для определения hasNextPage
	comments, err := r.Storage.GetCommentsByPostID(ctx, obj.ID, storage.PaginationArgs{Limit: l + 1, Cursor: cursor})
	if err != nil {
		return nil, fmt.Errorf("failed to get post comments: %w", err)
	}

	hasNextPage := len(comments) > l
	if hasNextPage {
		comments = comments[:l]
	}

	edges := make([]*model.CommentEdge, len(comments))
	for i, c := range comments {
		edges[i] = &model.CommentEdge{Node: c, Cursor: c.ID}
	}

	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.CommentConnection{
		Edges: edges,
		PageInfo: &model.PageInfo{
			HasNextPage: hasNextPage,
			EndCursor:   endCursor,
		},
	}, nil
}

// === Query Resolvers ===

func (r *queryResolver) Posts(ctx context.Context, limit *int, offset *int) ([]*domain.Post, error) {
	l, o := 10, 0
	if limit != nil {
		l = *limit
	}
	if offset != nil {
		o = *offset
	}
	return r.Storage.GetPosts(ctx, l, o)
}

func (r *queryResolver) Post(ctx context.Context, id string) (*domain.Post, error) {
	return r.Storage.GetPostByID(ctx, id)
}

// === Subscription Resolvers ===

func (r *subscriptionResolver) CommentAdded(ctx context.Context, postID string) (<-chan *domain.Comment, error) {
	// Проверяем, существует ли пост, прежде чем подписываться
	if _, err := r.Storage.GetPostByID(ctx, postID); err != nil {
		return nil, errors.New("post not found")
	}

	ch := make(chan *domain.Comment, 1)
	subID := uuid.NewString()

	r.Observer.mu.Lock()
	if r.Observer.subs[postID] == nil {
		r.Observer.subs[postID] = make(map[string]chan *domain.Comment)
	}
	r.Observer.subs[postID][subID] = ch
	r.Observer.mu.Unlock()

	// Горутина для очистки при отключении клиента
	go func() {
		<-ctx.Done()
		r.Observer.mu.Lock()
		if postSubs, ok := r.Observer.subs[postID]; ok {
			delete(postSubs, subID)
			if len(postSubs) == 0 {
				delete(r.Observer.subs, postID)
			}
		}
		r.Observer.mu.Unlock()
	}()

	return ch, nil
}

// === Boilerplate: Связывание резолверов с сгенерированным интерфейсом ===

// Comment returns generated.CommentResolver implementation.
func (r *Resolver) Comment() generated.CommentResolver { return &commentResolver{r} }

// Mutation returns generated.MutationResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

// Post returns generated.PostResolver implementation.
func (r *Resolver) Post() generated.PostResolver { return &postResolver{r} }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

// Subscription returns generated.SubscriptionResolver implementation.
func (r *Resolver) Subscription() generated.SubscriptionResolver { return &subscriptionResolver{r} }

type commentResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }
type postResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }
