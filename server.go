package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/clientv3/concurrency"
	"go.etcd.io/etcd/mvcc/mvccpb"
)

var (
	memberKeyPattern = regexp.MustCompile("/simple-ds/members/(.*)")
)

type ServerMeta struct {
	ID string
}

type Server struct {
	ServerMeta

	session       *concurrency.Session
	election      *concurrency.Election
	currentLeader string

	ctx    context.Context
	cancel context.CancelFunc
	sync.Mutex
}

func NewServer(id string, client *clientv3.Client) (*Server, error) {
	session, err := concurrency.NewSession(client, concurrency.WithTTL(30))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		ServerMeta: ServerMeta{
			ID: id,
		},

		session:  session,
		election: concurrency.NewElection(session, "/simple-ds/elections"),

		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (s *Server) isLeader() bool {
	return s.currentLeader == s.ID
}

func (s *Server) runForLeadership() error {
	log.Printf("%s running for leadership", s.ID)
	go func() {
		for evt := range s.election.Observe(s.ctx) {
			s.Lock()
			s.currentLeader = string(evt.Kvs[0].Value)
			s.Unlock()
		}
	}()
	return s.election.Campaign(s.ctx, s.ID)
}

func (s *Server) registerServer() error {
	memberKey := fmt.Sprintf("/simple-ds/members/%s", s.ID)
	memberInfo, err := json.Marshal(s.ServerMeta)
	if err != nil {
		return err
	}
	if _, err = s.session.Client().Put(s.ctx, memberKey, string(memberInfo), clientv3.WithLease(s.session.Lease())); err != nil {
		return err
	}
	return nil
}

func (s *Server) observeMemberChanges() {
	ch := s.session.Client().Watch(s.ctx, "/simple-ds/members", clientv3.WithPrefix())
	for wresp := range ch {
		for _, ev := range wresp.Events {
			matches := memberKeyPattern.FindSubmatch(ev.Kv.Key)
			if len(matches) != 2 {
				log.Printf("Unable to extract member id on member change event. Key: %q", ev.Kv.Key)
				continue
			}
			memberID := string(matches[1])
			switch ev.Type {
			case mvccpb.PUT:
				if memberID != s.ID {
					log.Printf("New member joined cluster. Member ID: %s", memberID)
				}
			case mvccpb.DELETE:
				if memberID != s.ID {
					log.Printf("Member leaved cluster. Member ID: %s", memberID)
				}
			}
		}
	}
}

func (s *Server) start() error {
	defer s.cleanup()
	if err := s.registerServer(); err != nil {
		return err
	}

	go s.runForLeadership()
	go s.observeMemberChanges()

	for {
		select {
		case <-s.ctx.Done():
			return nil
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
	s.cancel()
}

func (s *Server) cleanup() {
	if s.isLeader() {
		s.election.Resign(context.TODO())
	}
	s.session.Close()
}
