package main

import (
	"context"
	"log"
	"sync"
	"time"

	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/clientv3/concurrency"
)

type Server struct {
	id string

	session       *concurrency.Session
	election      *concurrency.Election
	currentLeader string

	stopCh chan bool
	sync.Mutex
}

func NewServer(id string, client *clientv3.Client) (*Server, error) {
	session, err := concurrency.NewSession(client, concurrency.WithTTL(5))
	if err != nil {
		return nil, err
	}
	return &Server{
		id:       id,
		session:  session,
		election: concurrency.NewElection(session, "/simple-ds"),
		stopCh:   make(chan bool, 1),
	}, nil
}

func (s *Server) isLeader() bool {
	return s.currentLeader == s.id
}

func (s *Server) runForLeadership(ctx context.Context) error {
	log.Printf("%s running for leadership", s.id)
	go func() {
		for evt := range s.election.Observe(ctx) {
			s.Lock()
			s.currentLeader = string(evt.Kvs[0].Value)
			s.Unlock()
		}
	}()
	return s.election.Campaign(ctx, s.id)
}

func (s *Server) start() {
	for {
		select {
		case <-s.stopCh:
			return
		default:
			if s.isLeader() {
				log.Println("Hooray, we are leader now!!!!")
			}
			time.Sleep(time.Second)
		}
	}
}

func (s *Server) stop() {
	log.Println("Received terminate signal. Resign leadership if possible")
	if s.isLeader() {
		s.election.Resign(context.TODO())
	}
	s.stopCh <- true
}
