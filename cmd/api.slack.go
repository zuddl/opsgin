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
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/spf13/viper"
)

var priority_auto_increase_alert map[string]bool

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
		priority_auto_increase_alert = make(map[string]bool)

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

func (s *Schedules) slackGetAttachmentAction(action ...string) []slack.AttachmentAction {
	actionList := []slack.AttachmentAction{}

	for _, item := range action {
		switch item {
		case "alert_increase_priority":
			action := slack.AttachmentAction{
				Name:  "alert_increase_priority",
				Style: "danger",
				Text:  "Increase priority",
				Type:  "button",
				Value: "alert_increase_priority",
			}

			if viper.GetBool("_opsgenie.priority_increase.confirm") {
				action.Confirm = &slack.ConfirmationField{
					Text: viper.GetString("_opsgenie.messages.alert_increase_priority.tip"),
				}
			}

			actionList = append(actionList, action)
		case "alert_acknowledge":
			actionList = append(actionList, slack.AttachmentAction{
				Name:  "alert_acknowledge",
				Text:  "Ack",
				Type:  "button",
				Value: "alert_acknowledge",
			})
		case "alert_close":
			actionList = append(actionList, slack.AttachmentAction{
				Name:  "alert_close",
				Style: "primary",
				Text:  "Close",
				Type:  "button",
				Value: "alert_close",
			})
		default:
			continue
		}
	}

	return actionList
}

func (s *Schedules) slackGetAttachmentFields(priority, duty string, priorityAutoIncrease int) []slack.AttachmentField {
	timeFormated := fmt.Sprintf("%02d:%02d", priorityAutoIncrease/60, priorityAutoIncrease%60)

	attachmentField := []slack.AttachmentField{
		{Short: true, Title: viper.GetString("_opsgenie.messages.fields.priority"), Value: priority},
		{Short: true, Title: viper.GetString("_opsgenie.messages.fields.on_duty"), Value: fmt.Sprintf("<@%s>", duty)},
	}

	if priorityAutoIncrease > 0 {
		attachmentField = append(
			attachmentField,
			slack.AttachmentField{
				Short: true,
				Title: strings.Replace(viper.GetString("_opsgenie.messages.fields.priority_p1_after"), "_time_", timeFormated, -1),
			},
		)
	}

	return attachmentField
}

func (s *Schedules) slackWatchEvents() {
	for envelope := range s.sm.Events {
		switch envelope.Type {
		case
			socketmode.EventTypeEventsAPI,
			socketmode.EventTypeInteractive,
			socketmode.EventTypeSlashCommand:
			s.log.Debugf("event type: %v", envelope.Type)
		default:
			s.log.Debugf("skipped: %v", envelope.Type)
			continue
		}

		s.sm.Ack(*envelope.Request)

		s.log.Debugf("getting schedule - %#v", s.list[0].name)
		if err := s.opsgenieGetSchedules(s.list[0].name); err != nil {
			s.log.Errorf("can't load schedule - %s", err.Error())

			continue
		}

		s.log.Debug("getting slack users")
		if err := s.slackFindUsers(); err != nil {
			s.log.Errorf("can't load slack users - %s", err.Error())

			continue
		}

		e := Event{
			OnDuty: s.list[0].duty[0],
		}

		switch envelope.Type {
		case socketmode.EventTypeInteractive:
			payload, _ := envelope.Data.(slack.InteractionCallback)

			switch payload.ActionCallback.AttachmentActions[0].Value {
			case
				"alert_acknowledge",
				"alert_close",
				"alert_increase_priority":
			default:
				continue
			}

			alert := strings.Split(payload.CallbackID, ";")

			e.Action = payload.ActionCallback.AttachmentActions[0].Value
			e.AlertID = alert[0]
			e.AlertPriority = alert[1]
			e.ChannelID = payload.Channel.GroupConversation.Conversation.ID
			e.ResponseURL = payload.ResponseURL
			e.UserID = payload.User.ID
			s.Interactive(e)

		case socketmode.EventTypeEventsAPI:
			payload, _ := envelope.Data.(slackevents.EventsAPIEvent)

			switch event := payload.InnerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				var msg slackevents.MessageEvent

				json.Unmarshal([]byte(*payload.Data.(*slackevents.EventsAPICallbackEvent).InnerEvent), &msg)

				if msg.Edited != nil {
					continue
				}

				e.ChannelID = event.Channel
				e.Data = event.Text
				e.ThreadTimeStamp = event.ThreadTimeStamp
				e.TimeStamp = event.TimeStamp
				s.EventsApi(e)
			}

		case socketmode.EventTypeSlashCommand:
			payload, _ := envelope.Data.(slack.SlashCommand)

			e.Action = "SlashCommand"
			e.ChannelID = payload.ChannelID
			e.Data = payload.Text
			e.UserID = payload.UserID
			s.SlashCommand(e)
		}
	}
}

func (s *Schedules) EventsApi(e Event) {
	var (
		priority_auto_increase = viper.GetInt("_opsgenie.priority_increase.timer")
		slackAttachmentAction  = s.slackGetAttachmentAction("alert_increase_priority", "alert_acknowledge", "alert_close")
		slackAttachmentField   = s.slackGetAttachmentFields(viper.GetString("_opsgenie.priority"), e.OnDuty, priority_auto_increase)
		slackAttachmentColor   = "warning"
		slackResponse          = viper.GetString("_opsgenie.messages.alert_create.success")
	)

	s.log.Debug("getting permalink")
	link, err := s.slack.GetPermalink(&slack.PermalinkParameters{
		Channel: e.ChannelID,
		Ts:      e.TimeStamp,
	})
	if err != nil {
		s.log.Errorf("can't get permalink - %s", err.Error())

		return
	}

	ts := e.TimeStamp
	if e.ThreadTimeStamp != "" {
		ts = e.ThreadTimeStamp
	}

	s.log.Debug("adding opsgenie alert")
	alertID, err := s.opsgenieAddAlert(e.Data, ts, link)
	if err != nil {
		slackAttachmentAction = []slack.AttachmentAction{}
		slackAttachmentField = []slack.AttachmentField{}
		slackResponse = viper.GetString("_opsgenie.messages.alert_create.failure")

		s.log.Errorf("can't create alert - %s", err.Error())
	}

	s.log.Debugf("sending slack.PostMessage to %s", e.UserID)
	_, respTS, err := s.slack.PostMessage(
		e.ChannelID,
		slack.MsgOptionTS(e.TimeStamp),
		slack.MsgOptionAttachments(slack.Attachment{
			Actions:    slackAttachmentAction,
			CallbackID: fmt.Sprintf("%s;%s", alertID, viper.GetString("_opsgenie.priority")),
			Color:      slackAttachmentColor,
			Fields:     slackAttachmentField,
			Text:       strings.Replace(slackResponse, "_user_", fmt.Sprintf("<@%s>", e.UserID), -1),
		}),
	)
	if err != nil {
		s.log.Error(err, respTS)
	}

	if priority_auto_increase > 0 {
		go s.AlertPriorityAutoIncrease(Event{
			AlertID:       alertID,
			ChannelID:     e.ChannelID,
			TimeStamp:     respTS,
			IncreaseTimer: priority_auto_increase,
			OnDuty:        e.OnDuty,
		})
	}
}

func (s *Schedules) AlertPriorityAutoIncrease(e Event) {
	var (
		slackAttachmentAction = s.slackGetAttachmentAction("alert_increase_priority", "alert_acknowledge", "alert_close")
		slackAttachmentColor  = "warning"
		slackAttachmentField  = s.slackGetAttachmentFields(viper.GetString("_opsgenie.priority"), s.list[0].duty[0], e.IncreaseTimer)
		slackResponse         = viper.GetString("_opsgenie.messages.alert_create.success")
	)

	priority := viper.GetString("_opsgenie.priority")
	priority_auto_increase_alert[e.AlertID] = true

	for curTime := e.IncreaseTimer; curTime >= 0; curTime-- {
		if !priority_auto_increase_alert[e.AlertID] {
			return
		}

		if curTime == 0 {
			slackAttachmentColor = "danger"
			priority = "P1"

			if err := s.opsgenieIncreaseAlertPriority(e.AlertID, priority); err != nil {
				slackResponse = viper.GetString("_opsgenie.messages.alert_increase_priority.failure")

				s.log.Errorf("can't close alert - %s", err.Error())
			} else {
				slackAttachmentAction = s.slackGetAttachmentAction("alert_acknowledge", "alert_close")
			}
		}

		slackAttachmentField = s.slackGetAttachmentFields(priority, e.OnDuty, curTime)

		if curTime%5 == 0 {
			_, _, _, err := s.slack.UpdateMessage(
				e.ChannelID,
				e.TimeStamp,
				slack.MsgOptionAttachments(slack.Attachment{
					Actions:    slackAttachmentAction,
					CallbackID: fmt.Sprintf("%s;%s", e.AlertID, priority),
					Color:      slackAttachmentColor,
					Fields:     slackAttachmentField,
					Text:       slackResponse,
				}),
			)
			if err != nil {
				s.log.Error(err)
			}
		}

		time.Sleep(1 * time.Second)
	}
}

func (s *Schedules) Interactive(e Event) {
	var (
		slackAttachmentAction []slack.AttachmentAction
		slackAttachmentColor  string
		slackAttachmentField  = s.slackGetAttachmentFields(e.AlertPriority, e.OnDuty, 0)
		slackResponse         string
	)

	delete(priority_auto_increase_alert, e.AlertID)

	switch e.Action {
	case "alert_close":
		slackAttachmentColor = "good"
		slackResponse = viper.GetString("_opsgenie.messages.alert_close.success")

		if err := s.opsgenieCloseAlert(e.AlertID); err != nil {
			slackResponse = viper.GetString("_opsgenie.messages.alert_close.failure")

			s.log.Errorf("can't close alert - %s", err.Error())
		} else {
			slackAttachmentAction = []slack.AttachmentAction{}
		}
	case "alert_acknowledge":
		slackAttachmentColor = "#039be5"
		slackResponse = viper.GetString("_opsgenie.messages.alert_acknowledged.success")

		if err := s.opsgenieAckAlert(e.AlertID); err != nil {
			slackResponse = viper.GetString("_opsgenie.messages.alert_acknowledged.failure")

			s.log.Errorf("can't ack alert - %s", err.Error())
		} else {
			slackAttachmentAction = s.slackGetAttachmentAction("alert_close")
		}
	case "alert_increase_priority":
		e.AlertPriority = "P1"

		slackAttachmentField = s.slackGetAttachmentFields(e.AlertPriority, s.list[0].duty[0], 0)
		slackAttachmentColor = "danger"
		slackResponse = viper.GetString("_opsgenie.messages.alert_increase_priority.success")

		if err := s.opsgenieIncreaseAlertPriority(e.AlertID, e.AlertPriority); err != nil {
			slackResponse = viper.GetString("_opsgenie.messages.alert_increase_priority.failure")

			s.log.Errorf("can't close alert - %s", err.Error())
		} else {
			slackAttachmentAction = s.slackGetAttachmentAction("alert_acknowledge", "alert_close")
		}
	}

	if _, _, err := s.slack.PostMessage(
		e.ChannelID,
		slack.MsgOptionReplaceOriginal(e.ResponseURL),
		slack.MsgOptionAttachments(slack.Attachment{
			Actions:    slackAttachmentAction,
			CallbackID: fmt.Sprintf("%s;%s", e.AlertID, e.AlertPriority),
			Color:      slackAttachmentColor,
			Fields:     slackAttachmentField,
			Text:       strings.Replace(slackResponse, "_user_", fmt.Sprintf("<@%s>", e.UserID), -1),
		}),
	); err != nil {
		s.log.Error(err)
	}
}

func (s *Schedules) SlashCommand(e Event) {
	response := ""

	switch e.Data {
	case "w", "who":
		response = viper.GetString("_opsgenie.messages.command.on_duty")
	case "":
		response = viper.GetString("_opsgenie.messages.command.help")
	default:
		response = viper.GetString("_opsgenie.messages.command.unknown")
	}

	if _, err := s.slack.PostEphemeral(
		e.ChannelID,
		e.UserID,
		slack.MsgOptionText(
			strings.Replace(
				response,
				"_user_",
				fmt.Sprintf("<@%s>", e.OnDuty),
				-1,
			),
			false,
		),
	); err != nil {
		s.log.Error(err)
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
