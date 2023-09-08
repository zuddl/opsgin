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
	"context"
	"fmt"
	"time"

	"github.com/opsgenie/opsgenie-go-sdk-v2/alert"
	"github.com/opsgenie/opsgenie-go-sdk-v2/client"
	"github.com/opsgenie/opsgenie-go-sdk-v2/schedule"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func (s *Schedules) opsgenieInitSchedule() error {
	if s.sc != nil {
		return nil
	}

	api_key := s.list[0].og_api_key

	if api_key == "" {
		if api_key = viper.GetString("api.key"); api_key == "" {
			return fmt.Errorf("opsgenie API key is empty")
		}
	}

	sc, err := schedule.NewClient(&client.Config{
		ApiKey:     api_key,
		Logger:     log.StandardLogger(),
		RetryCount: 5,
	})
	if err != nil {
		log.Fatal("failed to create a client")
	}

	s.sc = sc

	return nil
}

func (s *Schedules) opsgenieInitAlert() error {
	if s.ac != nil {
		return nil
	}

	api_key := s.list[0].og_api_key

	if api_key == "" {
		if api_key = viper.GetString("api.key"); api_key == "" {
			return fmt.Errorf("opsgenie API key is empty")
		}
	}

	ac, err := alert.NewClient(&client.Config{
		ApiKey:     api_key,
		Logger:     log.StandardLogger(),
		RetryCount: 5,
	})
	if err != nil {
		log.Fatal("failed to create a client")
	}

	s.ac = ac

	return nil
}

func (s *Schedules) opsgenieGetSchedules(sn ...string) error {
	if err := s.opsgenieInitSchedule(); err != nil {
		return err
	}

	ctx := context.Background()

	for idx, item := range s.list {
		if len(sn) > 0 && sn[0] != item.name {
			continue
		}

		log.Infof("Schedule loading: %s", item.name)

		s.list[idx].duty = []string{}

		flat := false
		oc, err := s.sc.GetOnCalls(ctx, &schedule.GetOnCallsRequest{
			Flat:                   &flat,
			ScheduleIdentifier:     item.name,
			ScheduleIdentifierType: schedule.Name,
		})

		if err != nil {
			continue
		}

		for _, participants := range oc.OnCallParticipants {
			s.list[idx].duty = append(s.list[idx].duty, participants.Name)
		}
	}

	return nil
}

func (s *Schedules) opsgenieOverrideSchedules(user string, duration time.Duration) error {
	if err := s.opsgenieInitSchedule(); err != nil {
		return err
	}

	ctx := context.Background()

	if _, err := s.sc.CreateScheduleOverride(ctx, &schedule.CreateScheduleOverrideRequest{
		EndDate:                time.Now().Add(duration),
		StartDate:              time.Now(),
		ScheduleIdentifier:     s.list[0].name,
		ScheduleIdentifierType: schedule.Name,
		User: schedule.Responder{
			Type:     schedule.UserResponderType,
			Username: user,
		},
	}); err != nil {
		return err
	}

	return nil
}

func (s *Schedules) opsgenieAddAlert(message, thread_ts, thread_link string) (string, error) {
	if err := s.opsgenieInitAlert(); err != nil {
		return "", err
	}

	ctx := context.Background()

	res, err := s.ac.Create(ctx, &alert.CreateAlertRequest{
		Description: fmt.Sprintf("slack:%s\n%s", thread_link, message),
		Message:     "you were called in the slack",
		Priority:    alert.Priority(viper.GetString("_opsgenie.priority")),
		Responders: []alert.Responder{{
			Name: s.list[0].name,
			Type: alert.ScheduleResponder,
		}},
		Tags: []string{pkg, s.list[0].group},
	})
	if err != nil {
		s.log.Error("failed to create an alert")
		return "", err
	}

	req, err := res.RetrieveStatus(ctx)
	if err != nil {
		s.log.Error("failed to get an alert")
		return "", err
	}

	return req.AlertID, nil
}

func (s *Schedules) opsgenieCloseAlert(alertID string) error {
	if err := s.opsgenieInitAlert(); err != nil {
		return err
	}

	ctx := context.Background()

	if _, err := s.ac.Close(ctx, &alert.CloseAlertRequest{
		IdentifierType:  alert.ALERTID,
		IdentifierValue: alertID,
	}); err != nil {
		return err
	}

	return nil
}

func (s *Schedules) opsgenieAckAlert(alertID string) error {
	if err := s.opsgenieInitAlert(); err != nil {
		return err
	}

	ctx := context.Background()

	if _, err := s.ac.Acknowledge(ctx, &alert.AcknowledgeAlertRequest{
		IdentifierType:  alert.ALERTID,
		IdentifierValue: alertID,
	}); err != nil {
		return err
	}

	return nil
}

func (s *Schedules) opsgenieIncreaseAlertPriority(alertID, priority string) error {
	if err := s.opsgenieInitAlert(); err != nil {
		return err
	}

	ctx := context.Background()

	if _, err := s.ac.UpdatePriority(ctx, &alert.UpdatePriorityRequest{
		IdentifierType:  alert.ALERTID,
		IdentifierValue: alertID,
		Priority:        alert.Priority(priority),
	}); err != nil {
		return err
	}

	return nil
}
