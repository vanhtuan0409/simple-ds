package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.etcd.io/etcd/clientv3"
)

func main() {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: time.Second,
	})
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	id := fmt.Sprintf("client-%d", os.Getpid())
	s, err := NewServer(id, cli)
	if err != nil {
		panic(err)
	}

	go s.runForLeadership()
	go s.observeMemberChanges()

	termCh := make(chan os.Signal, 1)
	signal.Notify(termCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-termCh
		s.stop()
	}()

	s.start()
}
