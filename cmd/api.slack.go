/*
Copyright © 2022 Denis Halturin <dhalturin@hotmail.com>
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
   this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
POSSIBILITY OF SUCH DAMAGE.
*/
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/spf13/viper"
)

func (s *Schedules) slackInit() error {
	if s.slack != nil {
		return nil
	}

	var (
		slack_app_key string // xapp
		slack_key     string // xoxp or xoxb
		slack_type    string
	)

	switch s.mode {
	case "daemon":
		for _, item := range s.list[0].token {
			if ok, _ := regexp.MatchString(`xox[p,b]-`, item); ok {
				slack_key = item
			}

			if strings.Contains(item, "xapp-") {
				slack_app_key = item
			}
		}

		slack_type = "appname"
	case "sync":
		slack_key = viper.GetString("slack.api.key")
		slack_type = "usergroup"
	default:
		return fmt.Errorf("unknown app mode")
	}

	s.log = log.WithFields(log.Fields{
		slack_type: s.list[0].group,
		"schedule": s.list[0].name,
	})

	s.log.Info("init slack client")

	s.slack = slack.New(
		slack_key,
		slack.OptionAppLevelToken(slack_app_key),
		// slack.OptionDebug(true),
	)

	return nil
}

func (s *Schedules) slackClientsWS() error {
	for _, item := range s.list {
		schedule := Schedules{
			list: []Schedule{item},
			mode: s.mode,
		}

		if err := schedule.slackConnectToWS(); err != nil {
			return err
		}
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigs

	log.WithFields(log.Fields{
		"signal": sig.String(),
		"code":   fmt.Sprintf("%d", sig),
	}).Info("Signal notify")

	return nil
}

func (s *Schedules) slackConnectToWS() error {
	if err := s.slackInit(); err != nil {
		return err
	}

	if _, err := s.slack.AuthTest(); err != nil {
		return err
	}

	s.sm = socketmode.New(
		s.slack,
		// socketmode.OptionDebug(true),
	)

	go s.slackWatchEvents()

	go s.sm.Run()

	return nil
}

func (s *Schedules) slackWatchEvents() {
	for envelope := range s.sm.Events {
		s.log.Debugf(">> %#v\n", envelope)

		switch envelope.Type {
		case socketmode.EventTypeInteractive:
			s.sm.Ack(*envelope.Request)

			payload, _ := envelope.Data.(slack.InteractionCallback)

			if _, _, err := s.slack.PostMessage(
				payload.Channel.GroupConversation.Conversation.ID,
				slack.MsgOptionDeleteOriginal(payload.ResponseURL),
			); err != nil {
				s.log.Error(err)
			}

			data := strings.Split(payload.CallbackID, ";")

			slack_response := ":dizzy: Alert `%s` was closed"

			if err := s.opsgenieCloseAlert(data[0]); err != nil {
				slack_response = ":bangbang: Failed to close alert `%s`"

				s.log.Errorf("can't close alert - %s", err.Error())
			}

			if _, err := s.slack.PostEphemeral(
				payload.Channel.GroupConversation.Conversation.ID,
				payload.User.ID,
				slack.MsgOptionTS(data[1]),
				slack.MsgOptionText(
					fmt.Sprintf(slack_response, data[0]),
					false,
				),
			); err != nil {
				s.log.Error(err)
			}

		case socketmode.EventTypeEventsAPI:
			s.sm.Ack(*envelope.Request)

			payload, _ := envelope.Data.(slackevents.EventsAPIEvent)

			switch event := payload.InnerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				l := s.log.WithFields(log.Fields{"event_user": event.User})

				l.Debug("getting permalink")
				link, err := s.slack.GetPermalink(&slack.PermalinkParameters{
					Channel: event.Channel,
					Ts:      event.TimeStamp,
				})
				if err != nil {
					l.Errorf("can't get permalink - %s", err.Error())

					continue
				}

				l.Debugf("getting schedule - %#v", s.list[0].name)
				if err := s.opsgenieGetSchedules(s.list[0].name); err != nil {
					l.Errorf("can't load schedule - %s", err.Error())

					continue
				}

				l.Debug("getting slack users")
				if err := s.slackFindUsers(); err != nil {
					l.Errorf("can't load slack users - %s", err.Error())

					continue
				}

				slack_response := ":wave: Hi, <@%s>! <@%s> has been notified and will be coming soon"

				ts := event.TimeStamp
				if event.ThreadTimeStamp != "" {
					ts = event.ThreadTimeStamp
				}

				l.Debug("adding opsgenie alert")
				alertID, err := s.opsgenieAddAlert(event.Text, ts, link)
				if err != nil {
					slack_response = ":wave: Hi, <@%s>! I couldn't create an alert in OpsGenie :sob: <@%s>"

					l.Errorf("can't create alert - %s", err.Error())
				}

				l.Debugf("sending slack.PostMessage to %s", event.User)
				if _, _, err := s.slack.PostMessage(
					event.Channel,
					slack.MsgOptionTS(event.TimeStamp),
					slack.MsgOptionText(
						fmt.Sprintf(
							slack_response,
							event.User,
							s.list[0].duty[0],
						),
						false,
					),
				); err != nil {
					l.Error(err)
				}

				l.Debugf("sending slack.PostEphemeral to %s", s.list[0].duty[0])
				if _, err := s.slack.PostEphemeral(
					event.Channel,
					s.list[0].duty[0],
					slack.MsgOptionTS(event.TimeStamp),
					slack.MsgOptionAttachments(slack.Attachment{
						Actions: []slack.AttachmentAction{
							{Name: "alert_close", Text: "close", Type: "button"},
						},
						CallbackID: fmt.Sprintf("%s;%s", alertID, event.TimeStamp),
						Color:      "good",
						Text:       "You can close this alert in Opsgenie",
					}),
				); err != nil {
					l.Error(err)
				}
			}
		default:
			s.log.Debugf("skipped: %v", envelope.Type)
		}
	}
}

func (s *Schedules) slackUpdateUserGroup() error {
	if err := s.slackInit(); err != nil {
		return err
	}

	if err := s.slackGetUserGroups(); err != nil {
		log.Fatal(err)
	}

	if err := s.slackFindUsers(); err != nil {
		log.Fatal(err)
	}

	for _, item := range s.list {
		if item.group == "" {
			continue
		}

		duty := []string{}

		for _, uid := range item.duty {
			if uid == "" {
				continue
			}

			duty = append(duty, uid)
		}

		if len(duty) < 1 {
			s.log.Warn("there are no on-duty on this calendar")

			continue
		}

		if _, err := s.slack.UpdateUserGroupMembers(item.group, strings.Join(duty, ",")); err != nil {
			s.log.Error(err)

			continue
		}

		s.log.Infof("the user group has been updated")
	}

	return nil
}

func (s *Schedules) slackGetUserGroups() error {
	if err := s.slackInit(); err != nil {
		return err
	}

	s.groups = make(map[string]string)

	groups, err := s.slack.GetUserGroups()
	if err != nil {
		return err
	}

	for _, group := range groups {
		s.groups[group.Handle] = group.ID
	}

	s.log.Debugf("slack user groups: %#v", s.groups)

	for idx, item := range s.list {
		if len(item.duty) < 1 {
			continue
		}

		group, ok := s.groups[item.group]
		if !ok {
			s.log.Errorf("can't find group id")
		}

		s.list[idx].group = group
	}

	return nil
}

func (s *Schedules) slackFindUsers() error {
	if err := s.slackInit(); err != nil {
		return err
	}

	for _, item := range s.list {
		if len(item.duty) < 1 {
			continue
		}

		for idx, duty := range item.duty {
			user, err := s.slack.GetUserByEmail(duty)
			if err != nil {
				s.log.Warnf("can't find user %#v", duty)

				user = &slack.User{} // the user will be removed from duty
			}

			item.duty[idx] = user.ID
		}
	}

	return nil
}
