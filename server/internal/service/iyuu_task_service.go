package service

import torrentdatapkg "github.com/pt-nexus/server/internal/service/torrentdata"

type IYUUTaskService = torrentdatapkg.IYUUTaskService

func NewIYUUTaskService() *IYUUTaskService {
	return torrentdatapkg.NewIYUUTaskService()
}
