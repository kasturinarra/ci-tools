package steps

import (
	"context"
	"fmt"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	prowapi "sigs.k8s.io/prow/pkg/apis/prowjobs/v1"

	buildapi "github.com/openshift/api/build/v1"

	"github.com/openshift/ci-tools/pkg/api"
	"github.com/openshift/ci-tools/pkg/kubernetes"
	"github.com/openshift/ci-tools/pkg/metrics"
	"github.com/openshift/ci-tools/pkg/results"
)

type gitSourceStep struct {
	config          api.ProjectDirectoryImageBuildInputs
	resources       api.ResourceConfiguration
	buildClient     BuildClient
	podClient       kubernetes.PodClient
	jobSpec         *api.JobSpec
	cloneAuthConfig *CloneAuthConfig
	pullSecret      *coreapi.Secret
	architectures   sets.Set[string]
	metricsAgent    *metrics.MetricsAgent
}

func (s *gitSourceStep) Inputs() (api.InputDefinition, error) {
	return s.jobSpec.Inputs(), nil
}

func (*gitSourceStep) Validate() error { return nil }

func (s *gitSourceStep) Run(ctx context.Context) error {
	return results.ForReason("building_image_from_source").ForError(s.run(ctx))
}

func (s *gitSourceStep) run(ctx context.Context) error {
	if refs := s.determineRefsWorkdir(s.jobSpec.Refs, s.jobSpec.ExtraRefs); refs != nil {
		cloneURI := fmt.Sprintf("https://github.com/%s/%s.git", refs.Org, refs.Repo)
		var secretName string
		if s.cloneAuthConfig != nil {
			cloneURI = s.cloneAuthConfig.getCloneURI(refs.Org, refs.Repo)
			secretName = s.cloneAuthConfig.Secret.Name
		}

		root := string(api.PipelineImageStreamTagReferenceRoot)
		if s.config.Ref != "" {
			root = fmt.Sprintf("%s-%s", root, s.config.Ref)
		}
		return handleBuilds(ctx, s.buildClient, s.podClient, *buildFromSource(s.jobSpec, "", api.PipelineImageStreamTagReference(root), buildapi.BuildSource{
			Type:         buildapi.BuildSourceGit,
			Dockerfile:   s.config.DockerfileLiteral,
			ContextDir:   s.config.ContextDir,
			SourceSecret: getSourceSecretFromName(secretName),
			Git: &buildapi.GitBuildSource{
				URI: cloneURI,
				Ref: refs.BaseRef,
			},
		}, "", s.config.DockerfilePath, s.resources, s.pullSecret, nil, s.config.Ref), s.metricsAgent, newImageBuildOptions(s.architectures.UnsortedList()))
	}

	return fmt.Errorf("nothing to build source image from, no refs")
}

func (s *gitSourceStep) Name() string {
	root := string(api.PipelineImageStreamTagReferenceRoot)
	if s.config.Ref != "" {
		root = fmt.Sprintf("%s-%s", root, s.config.Ref)
	}
	return root
}

func (s *gitSourceStep) Description() string {
	return fmt.Sprintf("Build git source code into an image and tag it as %s", api.PipelineImageStreamTagReferenceRoot)
}

func (s *gitSourceStep) Requires() []api.StepLink { return nil }

func (s *gitSourceStep) Creates() []api.StepLink {
	root := string(api.PipelineImageStreamTagReferenceRoot)
	if s.config.Ref != "" {
		root = fmt.Sprintf("%s-%s", root, s.config.Ref)
	}
	return []api.StepLink{api.InternalImageLink(api.PipelineImageStreamTagReference(root))}
}

func (s *gitSourceStep) Provides() api.ParameterMap {
	return nil
}

func (s *gitSourceStep) Objects() []ctrlruntimeclient.Object {
	return s.buildClient.Objects()
}

func (s *gitSourceStep) determineRefsWorkdir(refs *prowapi.Refs, extraRefs []prowapi.Refs) *prowapi.Refs {
	var totalRefs []prowapi.Refs

	if refs != nil {
		totalRefs = append(totalRefs, *refs)
	}
	totalRefs = append(totalRefs, extraRefs...)

	if len(totalRefs) == 0 {
		return nil
	}

	matchingRef := &totalRefs[0]
	for i, ref := range totalRefs {
		orgRepo := fmt.Sprintf("%s.%s", ref.Org, ref.Repo)
		matches := s.config.Ref == orgRepo
		if (s.config.Ref == "" || matches) && ref.WorkDir {
			return &totalRefs[i]
		}
		if matches {
			matchingRef = &totalRefs[i]
		}
	}

	return matchingRef
}

func (s *gitSourceStep) ResolveMultiArch() sets.Set[string] {
	return s.architectures
}

func (s *gitSourceStep) AddArchitectures(archs []string) {
	s.architectures.Insert(archs...)
}

// GitSourceStep returns gitSourceStep that holds all the required information to create a build from a git source.
func GitSourceStep(
	config api.ProjectDirectoryImageBuildInputs,
	resources api.ResourceConfiguration,
	buildClient BuildClient,
	podClient kubernetes.PodClient,
	jobSpec *api.JobSpec,
	cloneAuthConfig *CloneAuthConfig,
	pullSecret *coreapi.Secret,
	metricsAgent *metrics.MetricsAgent,
) api.Step {
	return &gitSourceStep{
		config:          config,
		resources:       resources,
		buildClient:     buildClient,
		podClient:       podClient,
		jobSpec:         jobSpec,
		cloneAuthConfig: cloneAuthConfig,
		pullSecret:      pullSecret,
		architectures:   sets.New[string](),
		metricsAgent:    metricsAgent,
	}
}
