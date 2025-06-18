package server

import (
	"GOMinifyURL/internal/proto"
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StorageServer struct {
	db *pgxpool.Pool
	proto.UnimplementedURLStorageServer
}

func NewStorageServer(db *pgxpool.Pool) *StorageServer {
	return &StorageServer{
		db: db,
	}
}

func (s StorageServer) PutURL(ctx context.Context, request *proto.ShortURLRequest) (*proto.ShortUrlResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (s StorageServer) GetFullURL(ctx context.Context, request *proto.FullUrlRequest) (*proto.FullUrlResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (s StorageServer) mustEmbedUnimplementedURLStorageServer() {
	//TODO implement me
	panic("implement me")
}
