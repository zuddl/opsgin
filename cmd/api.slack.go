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
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/spf13/viper"
)

func (s *Schedules) slackInit() error {
	slack_key := viper.GetString("slack.api.key")
	if slack_key == "" {
		return fmt.Errorf("slack API key is empty")
	}

	s.slack = slack.New(slack_key)

	return nil
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

	for _, item := range s.List {
		if item.Group == "" {
			continue
		}

		duty := []string{}

		for _, uid := range item.Duty {
			if uid == "" {
				continue
			}

			duty = append(duty, uid)
		}

		if len(duty) < 1 {
			log.WithFields(log.Fields{
				"usergroup": item.Group,
				"schedule":  item.Name,
			}).Warn("there are no on-duty on this calendar")

			continue
		}

		if _, err := s.slack.UpdateUserGroupMembers(item.Group, strings.Join(duty, ",")); err != nil {
			log.WithFields(log.Fields{
				"usergroup": item.Group,
				"schedule":  item.Name,
			}).Error(err)

			continue
		}

		log.WithFields(log.Fields{
			"usergroup": item.Group,
			"schedule":  item.Name,
		}).Infof("the user group has been updated")
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

	log.Debugf("slack user groups: %#v", s.groups)

	for idx, item := range s.List {
		if len(item.Duty) < 1 {
			continue
		}

		group, ok := s.groups[item.Group]
		if !ok {
			log.WithFields(log.Fields{
				"usergroup": item.Group,
				"schedule":  item.Name,
			}).Errorf("can't find group id")
		}

		s.List[idx].Group = group
	}

	return nil
}

func (s *Schedules) slackFindUsers() error {
	if err := s.slackInit(); err != nil {
		return err
	}

	for _, item := range s.List {
		if len(item.Duty) < 1 {
			continue
		}

		for idx, duty := range item.Duty {
			user, err := s.slack.GetUserByEmail(duty)
			if err != nil {
				log.WithFields(log.Fields{
					"usergroup": item.Group,
					"schedule":  item.Name,
				}).Warnf("can't find user %#v", duty)

				user = &slack.User{} // the user will be removed from duty
			}

			item.Duty[idx] = user.ID
		}
	}

	return nil
}
