package store

import "github.com/motao123/Argus/server/internal/model"

func testServer(id int64) *model.Server {
	return &model.Server{ID: id, Name: "t"}
}
