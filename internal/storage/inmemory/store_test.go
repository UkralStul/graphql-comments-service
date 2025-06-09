// internal/storage/inmemory/store_test.go

package inmemory

import (
	"context"
	"strings"
	"testing"

	// ЗАМЕНИТЕ НА ВАШ ПУТЬ
	"github.com/UkralStul/graphql-comments-service/internal/domain"
	"github.com/UkralStul/graphql-comments-service/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore создает хранилище и один пост для тестов
func newTestStore(t *testing.T) (storage.Storage, *domain.Post) {
	store := New()
	ctx := context.Background()
	post, err := store.CreatePost(ctx, &domain.Post{
		Title:           "Test Post",
		Content:         "Content",
		AuthorID:        "user-1",
		CommentsEnabled: true,
	})
	require.NoError(t, err)
	return store, post
}

func TestStore_CreateAndGetPost(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	retrieved, err := store.GetPostByID(ctx, post.ID)
	require.NoError(t, err)
	assert.Equal(t, post.Title, retrieved.Title)

	_, err = store.GetPostByID(ctx, "non-existent-id")
	assert.Error(t, err)
}

func TestStore_CreateComment_Success(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	comment, err := store.CreateComment(ctx, &domain.Comment{PostID: post.ID, AuthorID: "user-2", Content: "First comment!"})
	require.NoError(t, err)
	assert.NotEmpty(t, comment.ID)

	comments, err := store.GetCommentsByPostID(ctx, post.ID, storage.PaginationArgs{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, comments, 1)
	assert.Equal(t, "First comment!", comments[0].Content)
}

func TestStore_CreateComment_CommentsDisabled(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	// Отключаем комментарии
	_, err := store.ToggleComments(ctx, post.ID, false)
	require.NoError(t, err)

	// Пытаемся создать комментарий
	_, err = store.CreateComment(ctx, &domain.Comment{PostID: post.ID, AuthorID: "user-2", Content: "This should fail"})
	require.Error(t, err)
	assert.Equal(t, "comments are disabled for this post", err.Error())
}

func TestStore_CreateComment_TooLong(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	longContent := strings.Repeat("a", 2001)
	_, err := store.CreateComment(ctx, &domain.Comment{PostID: post.ID, AuthorID: "user-2", Content: longContent})
	require.Error(t, err)
	assert.Equal(t, "comment content is too long", err.Error())
}

func TestStore_CreateComment_EmptyContent(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	_, err := store.CreateComment(ctx, &domain.Comment{PostID: post.ID, AuthorID: "user-2", Content: "  "})
	require.Error(t, err)
	assert.Equal(t, "comment content cannot be empty", err.Error())
}

func TestStore_CreateNestedComment(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	parentComment, err := store.CreateComment(ctx, &domain.Comment{PostID: post.ID, AuthorID: "user-2", Content: "Parent"})
	require.NoError(t, err)

	childComment, err := store.CreateComment(ctx, &domain.Comment{PostID: post.ID, ParentID: &parentComment.ID, AuthorID: "user-3", Content: "Child"})
	require.NoError(t, err)

	// Проверяем, что дочерний коммент не в корне поста
	rootComments, err := store.GetCommentsByPostID(ctx, post.ID, storage.PaginationArgs{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, rootComments, 1)
	assert.Equal(t, parentComment.ID, rootComments[0].ID)

	// Проверяем, что дочерний коммент находится у родителя
	children, err := store.GetCommentsByParentID(ctx, parentComment.ID, storage.PaginationArgs{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, childComment.ID, children[0].ID)
}

func TestStore_Pagination(t *testing.T) {
	store, post := newTestStore(t)
	ctx := context.Background()

	// Создаем 5 комментариев
	for i := 0; i < 5; i++ {
		_, err := store.CreateComment(ctx, &domain.Comment{PostID: post.ID, AuthorID: "user-1", Content: "some comment"})
		require.NoError(t, err)
	}

	// Запрашиваем первую страницу из 2-х комментариев
	firstPage, err := store.GetCommentsByPostID(ctx, post.ID, storage.PaginationArgs{Limit: 2})
	require.NoError(t, err)
	require.Len(t, firstPage, 2)

	// Запрашиваем вторую страницу из 3-х, используя курсор
	cursor := firstPage[1].ID // курсор - это ID последнего элемента на предыдущей странице
	secondPage, err := store.GetCommentsByPostID(ctx, post.ID, storage.PaginationArgs{Limit: 3, Cursor: &cursor})
	require.NoError(t, err)
	require.Len(t, secondPage, 3)

	// Убеждаемся, что ID не пересекаются
	assert.NotEqual(t, firstPage[0].ID, secondPage[0].ID)
	assert.NotEqual(t, firstPage[1].ID, secondPage[0].ID)
}
