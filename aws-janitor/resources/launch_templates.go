/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// LaunchTemplates https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeLaunchTemplates
type LaunchTemplates struct{}

func (LaunchTemplates) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*launchTemplate // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *ec2.DescribeLaunchTemplatesOutput, _ bool) bool {
		for _, lt := range page.LaunchTemplates {
			l := &launchTemplate{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *lt.LaunchTemplateId,
				Name:    *lt.LaunchTemplateName,
			}
			if !set.Mark(opts, l, lt.CreateTime, fromEC2Tags(lt.Tags)) {
				continue
			}
			logger.Warningf("%s: deleting %T: %s", l.ARN(), lt, l.Name)
			if !opts.DryRun {
				toDelete = append(toDelete, l)
			}
		}
		return true
	}

	if err := svc.DescribeLaunchTemplatesPages(&ec2.DescribeLaunchTemplatesInput{}, pageFunc); err != nil {
		return err
	}

	for _, lt := range toDelete {
		deleteReq := &ec2.DeleteLaunchTemplateInput{
			LaunchTemplateId: aws.String(lt.ID),
		}

		if _, err := svc.DeleteLaunchTemplate(deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", lt.ARN(), err)
		}
	}

	return nil
}

func (LaunchTemplates) ListAll(opts Options) (*Set, error) {
	c := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &ec2.DescribeLaunchTemplatesInput{}

	err := c.DescribeLaunchTemplatesPages(input, func(lts *ec2.DescribeLaunchTemplatesOutput, isLast bool) bool {
		now := time.Now()
		for _, lt := range lts.LaunchTemplates {
			arn := launchTemplate{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *lt.LaunchTemplateId,
				Name:    *lt.LaunchTemplateName,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't list launch templates for %q in %q", opts.Account, opts.Region)
}

type launchTemplate struct {
	Account string
	Region  string
	ID      string
	Name    string
}

func (lt launchTemplate) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:launch-template/%s", lt.Region, lt.Account, lt.ID)
}

func (lt launchTemplate) ResourceKey() string {
	return lt.ARN()
}
