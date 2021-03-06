package awstasks

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/golang/glog"
	"k8s.io/kube-deploy/upup/pkg/fi"
	"k8s.io/kube-deploy/upup/pkg/fi/cloudup/awsup"
	"k8s.io/kube-deploy/upup/pkg/fi/cloudup/terraform"
	"k8s.io/kubernetes/pkg/util/diff"
	"net/url"
)

//go:generate fitask -type=IAMRolePolicy
type IAMRolePolicy struct {
	ID             *string
	Name           *string
	Role           *IAMRole
	PolicyDocument *fi.ResourceHolder
}

func (e *IAMRolePolicy) Find(c *fi.Context) (*IAMRolePolicy, error) {
	cloud := c.Cloud.(*awsup.AWSCloud)

	request := &iam.GetRolePolicyInput{
		RoleName:   e.Role.Name,
		PolicyName: e.Name,
	}

	response, err := cloud.IAM.GetRolePolicy(request)
	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == "NoSuchEntity" {
			return nil, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("error getting role: %v", err)
	}

	p := response
	actual := &IAMRolePolicy{}
	actual.Role = &IAMRole{Name: p.RoleName}
	if aws.StringValue(e.Role.Name) == aws.StringValue(p.RoleName) {
		actual.Role.ID = e.Role.ID
	}
	if p.PolicyDocument != nil {
		// The PolicyDocument is URI encoded (?)
		policy := *p.PolicyDocument
		policy, err = url.QueryUnescape(policy)
		if err != nil {
			return nil, fmt.Errorf("error parsing PolicyDocument for IAMRolePolicy %q: %v", e.Name, err)
		}
		actual.PolicyDocument = fi.WrapResource(fi.NewStringResource(policy))
	}
	actual.Name = p.PolicyName

	e.ID = actual.ID

	return actual, nil
}

func (e *IAMRolePolicy) Run(c *fi.Context) error {
	return fi.DefaultDeltaRunMethod(e, c)
}

func (s *IAMRolePolicy) CheckChanges(a, e, changes *IAMRolePolicy) error {
	if a != nil {
		if e.Name == nil {
			return fi.RequiredField("Name")
		}
	}
	return nil
}

func (_ *IAMRolePolicy) RenderAWS(t *awsup.AWSAPITarget, a, e, changes *IAMRolePolicy) error {
	policy, err := e.PolicyDocument.AsString()
	if err != nil {
		return fmt.Errorf("error rendering PolicyDocument: %v", err)
	}

	doPut := false

	if a == nil {
		glog.V(2).Infof("Creating IAMRolePolicy")
		doPut = true
	} else if changes != nil {
		if changes.PolicyDocument != nil {
			glog.V(2).Infof("Applying changed role policy to %q:", *e.Name)

			var err error

			actualPolicy := ""
			if a.PolicyDocument != nil {
				actualPolicy, err = a.PolicyDocument.AsString()
				if err != nil {
					return fmt.Errorf("error reading actual policy document: %v", err)
				}
			}

			if actualPolicy == policy {
				glog.Warning("Policies were actually the same")
			} else {
				d := diff.StringDiff(actualPolicy, policy)
				glog.V(2).Infof("diff: %s", d)
			}

			doPut = true
		}
	}

	if doPut {
		request := &iam.PutRolePolicyInput{}
		request.PolicyDocument = aws.String(policy)
		request.RoleName = e.Name
		request.PolicyName = e.Name

		_, err = t.Cloud.IAM.PutRolePolicy(request)
		if err != nil {
			return fmt.Errorf("error creating/updating IAMRolePolicy: %v", err)
		}
	}

	// TODO: Should we use path as our tag?
	return nil // No tags in IAM
}

type terraformIAMRolePolicy struct {
	Name           *string            `json:"name"`
	Role           *terraform.Literal `json:"role"`
	PolicyDocument *terraform.Literal `json:"policy"`
}

func (_ *IAMRolePolicy) RenderTerraform(t *terraform.TerraformTarget, a, e, changes *IAMRolePolicy) error {
	policy, err := t.AddFile("aws_iam_role_policy", *e.Name, "policy", e.PolicyDocument)
	if err != nil {
		return fmt.Errorf("error rendering PolicyDocument: %v", err)
	}

	tf := &terraformIAMRolePolicy{
		Name:           e.Name,
		Role:           e.Role.TerraformLink(),
		PolicyDocument: policy,
	}

	return t.RenderResource("aws_iam_role_policy", *e.Name, tf)
}

func (e *IAMRolePolicy) TerraformLink() *terraform.Literal {
	return terraform.LiteralSelfLink("aws_iam_role_policy", *e.Name)
}
