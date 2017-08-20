package party

import (
	"context"
	"io/ioutil"
	"net"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/memberlist"
	pb "github.com/partycloud/party/proto/daemon"
	"github.com/spf13/viper"
)

type Member struct {
	Name string
	IP   net.IP
}

// MemberList keeps list of connected members
type MemberList struct {
	Me      *Member
	Members map[string]*Member

	log *logrus.Entry
}

func (l *MemberList) NotifyJoin(node *memberlist.Node) {
	l.log.Infof("Join: %s %s\n", node.Name, node.Address())
	l.Members[node.Name] = &Member{
		Name: node.Name,
		IP:   node.Addr,
	}
}

func (l *MemberList) NotifyLeave(node *memberlist.Node) {
	l.log.Infof("Leave: %s %s\n", node.Name, node.Address())
	delete(l.Members, node.Name)
}

func (l *MemberList) NotifyUpdate(node *memberlist.Node) {
	l.log.Infof("Update: %s %s\n", node.Name, node.Address())
}

func (e *Environment) ListMembers(ctx context.Context, req *pb.ListMembersRequest) (*pb.ListMembersResponse, error) {
	members := make([]*pb.Member, len(e.MemberList.Members))
	i := 0
	for _, m := range e.MemberList.Members {
		members[i] = &pb.Member{
			Id:   m.Name, // @TODO get id
			Ip:   m.IP.String(),
			Name: m.Name,
		}
		i++
	}
	return &pb.ListMembersResponse{
		Members: members,
	}, nil
}

func RunMemberList(ctx context.Context) *MemberList {
	guildId := ""

	guildCh := make(chan *pb.Guild)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
				guild, err := ConnectedGuild(ctx)
				if err != nil {
					panic(err)
				}

				if guild != nil && guild.Id != guildId {
					// we changed guild
					guildId = guild.Id
					guildCh <- guild
				}
			}
		}
	}()

	memberList := &MemberList{
		Members: make(map[string]*Member),
	}

	name := viper.GetString("name")

	go func() {
		for guild := range guildCh {
			memberList.log = logrus.WithFields(logrus.Fields{"me": name})
			memberList.log.Infoln("starting gossip", guild)

			cfg := memberlist.DefaultLANConfig()
			cfg.LogOutput = ioutil.Discard
			cfg.BindAddr = guild.Ip
			cfg.BindPort = viper.GetInt("gossip-port")
			cfg.Events = memberList
			cfg.Name = name

			list, err := memberlist.Create(cfg)
			if err != nil {
				panic("Failed to create memberlist: " + err.Error())
			}
			memberList.log.Infoln("gossip listening", cfg.BindAddr, cfg.BindPort)
			peers := viper.GetStringSlice("peers")
			memberList.log.Infoln("finding friends", peers)

			join := func() error {
				_, err := list.Join(peers)
				return err
			}
			for {
				err = join()
				if err == nil {
					break
				}
				time.Sleep(1 * time.Second)
			}

			<-ctx.Done()
			memberList.log.Infoln("stopping gossip")
		}
	}()
	return memberList
}
