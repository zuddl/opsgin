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

	"github.com/opsgenie/opsgenie-go-sdk-v2/client"
	"github.com/opsgenie/opsgenie-go-sdk-v2/schedule"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func (s *Schedules) opsgenieGetSchedules() error {
	opsgin_key := viper.GetString("api.key")
	if opsgin_key == "" {
		return fmt.Errorf("opsgenie API key is empty")
	}

	ctx := context.Background()

	sc, err := schedule.NewClient(&client.Config{
		ApiKey: opsgin_key,
		Logger: log.StandardLogger(),
	})
	if err != nil {
		log.Fatal("failed to create a client")
	}

	for idx, item := range s.List {
		log.Infof("Schedule loading: %s", item.Name)

		flat := false
		oc, err := sc.GetOnCalls(ctx, &schedule.GetOnCallsRequest{
			Flat:                   &flat,
			ScheduleIdentifier:     item.Name,
			ScheduleIdentifierType: schedule.Name,
		})
		if err != nil {
			s.List[idx].Duty = []string{}

			continue
		}

		for _, participants := range oc.OnCallParticipants {
			s.List[idx].Duty = append(s.List[idx].Duty, participants.Name)
		}
	}

	return nil
}
