package dataloader

import (
	"context"
	"github.com/UkralStul/graphql-comments-service/internal/storage"
	"github.com/graph-gophers/dataloader"
	"net/http"
	"time"
)

type contextKey string

const key = contextKey("dataloaders")

// Loaders содержит все дата-лоадеры приложения.
type Loaders struct {
	ChildrenByCommentID *dataloader.Loader
}

// Middleware для внедрения лоадеров в контекст запроса.
func Middleware(store storage.Storage, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Создаем батч-функцию для лоадера
		batchFn := func(ctx context.Context, keys dataloader.Keys) []*dataloader.Result {
			// Преобразуем ключи в []string
			parentIDs := make([]string, len(keys))
			for i, key := range keys {
				parentIDs[i] = key.String()
			}

			// Вызываем метод хранилища, который делает ОДИН запрос к БД
			commentsMap, err := store.GetCommentsByParentIDs(ctx, parentIDs)
			if err != nil {
				// В случае ошибки, возвращаем ее для всех ключей
				results := make([]*dataloader.Result, len(keys))
				for i := range results {
					results[i] = &dataloader.Result{Error: err}
				}
				return results
			}

			// Формируем результат в том же порядке, что и ключи
			results := make([]*dataloader.Result, len(keys))
			for i, parentID := range parentIDs {
				results[i] = &dataloader.Result{Data: commentsMap[parentID]}
			}

			return results
		}

		// Создаем лоадеры
		loaders := Loaders{
			ChildrenByCommentID: dataloader.NewBatchedLoader(batchFn, dataloader.WithWait(time.Millisecond*1)),
		}

		// Помещаем их в контекст
		ctx := context.WithValue(r.Context(), key, &loaders)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// For извлекает лоадеры из контекста.
func For(ctx context.Context) *Loaders {
	return ctx.Value(key).(*Loaders)
}
