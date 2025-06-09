package main

import (
	"context"
	"flag"
	"github.com/UkralStul/graphql-comments-service/internal/domain"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/UkralStul/graphql-comments-service/graph"
	"github.com/UkralStul/graphql-comments-service/graph/generated"
	"github.com/UkralStul/graphql-comments-service/internal/dataloader"
	"github.com/UkralStul/graphql-comments-service/internal/storage"
	"github.com/UkralStul/graphql-comments-service/internal/storage/inmemory"
	"github.com/UkralStul/graphql-comments-service/internal/storage/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

const defaultPort = "8080"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	storageType := flag.String("storage", "in-memory", "Storage type (in-memory or postgres)")
	flag.Parse()

	var store storage.Storage
	var err error

	log.Printf("Starting server with %s storage", *storageType)
	if *storageType == "postgres" {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			log.Fatal("DATABASE_URL must be set for postgres storage")
		}
		store, err = postgres.New(dsn)
		if err != nil {
			log.Fatalf("failed to connect to postgres: %v", err)
		}
	} else {
		store = inmemory.New()
		// Заполним данными для тестов
		fillWithMockData(store)
	}

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	resolver := &graph.Resolver{
		Storage:  store,
		Observer: graph.NewCommentObserver(),
	}
	schema := generated.NewExecutableSchema(generated.Config{Resolvers: resolver})

	srv := handler.NewDefaultServer(schema)
	srv.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		KeepAlivePingInterval: 10 * time.Second,
	})

	router.Handle("/", playground.Handler("GraphQL playground", "/query"))
	router.Handle("/query", dataloader.Middleware(store, srv))

	log.Printf("connect to http://localhost:%s/ for GraphQL playground", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("server failed to start: %v", err)
	}
}

func fillWithMockData(s storage.Storage) {
	ctx := context.Background()

	// 1. Создаем пост и явно включаем комментарии. Проверяем ошибку.
	post, err := s.CreatePost(ctx, &domain.Post{
		Title:           "Тестовый пост о GraphQL",
		Content:         "Это содержимое тестового поста. Здесь мы обсуждаем GraphQL и Go.",
		AuthorID:        "user-1",
		CommentsEnabled: true,
	})
	if err != nil {
		log.Fatalf("fillWithMockData: failed to create post: %v", err)
	}

	// 2. Создаем первый корневой комментарий и проверяем ошибку.
	c1, err := s.CreateComment(ctx, &domain.Comment{
		PostID:   post.ID,
		AuthorID: "user-2",
		Content:  "Отличный пост! Очень информативно.",
	})
	if err != nil {
		log.Fatalf("fillWithMockData: failed to create comment 1: %v", err)
	}

	// 3. Создаем вложенный комментарий (ответ на первый) и проверяем ошибку.
	_, err = s.CreateComment(ctx, &domain.Comment{
		PostID:   post.ID,
		ParentID: &c1.ID, // Указываем родителя
		AuthorID: "user-1",
		Content:  "Спасибо! Рад, что вам понравилось.",
	})
	if err != nil {
		log.Fatalf("fillWithMockData: failed to create nested comment: %v", err)
	}

	// 4. Создаем второй корневой комментарий и проверяем ошибку.
	_, err = s.CreateComment(ctx, &domain.Comment{
		PostID:   post.ID,
		AuthorID: "user-3",
		Content:  "А как насчет производительности при большой вложенности?",
	})
	if err != nil {
		log.Fatalf("fillWithMockData: failed to create comment 2: %v", err)
	}

	// 5. Создаем еще один пост, но с выключенными комментариями для теста.
	disabledPost, err := s.CreatePost(ctx, &domain.Post{
		Title:           "Пост с выключенными комментариями",
		Content:         "К этому посту нельзя оставлять комментарии.",
		AuthorID:        "user-admin",
		CommentsEnabled: false, // <-- Явно выключаем комментарии
	})
	if err != nil {
		log.Fatalf("fillWithMockData: failed to create disabled post: %v", err)
	}

	log.Printf("Mock data filled successfully. Created post ID: %s, and post with disabled comments ID: %s", post.ID, disabledPost.ID)
}
