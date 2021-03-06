package controlapi

import (
	"strings"
	"testing"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/duration"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createSpec(name, image string, instances uint64) *api.ServiceSpec {
	return &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: name,
			Labels: map[string]string{
				"common": "yes",
				"unique": name,
			},
		},
		Task: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: image,
				},
			},
		},
		Mode: &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: instances,
			},
		},
	}
}

func createSpecWithDuplicateMounts(name string) *api.ServiceSpec {

	service := createSpec("", "image", 1)
	mounts := []api.Mount{
		{
			Target: "/foo",
			Source: "/mnt/mount1",
		},
		{
			Target: "/foo",
			Source: "/mnt/mount2",
		},
	}

	service.Task.GetContainer().Mounts = mounts

	return service
}

func createSpecWithDuplicateSecretTargets(name string) *api.ServiceSpec {
	service := createSpec(name, "image", 1)
	secretRefs := []*api.SecretReference{
		{
			SecretName: "secret1",
			SecretID:   "secretID1",
			Target:     "target.txt",
			Mode:       api.SecretReference_FILE,
		},
		{
			SecretName: "secret2",
			SecretID:   "secretID2",
			Target:     "target.txt",
			Mode:       api.SecretReference_FILE,
		},
	}

	service.Task.GetContainer().Secrets = secretRefs

	return service
}

func createService(t *testing.T, ts *testServer, name, image string, instances uint64) *api.Service {
	spec := createSpec(name, image, instances)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	return r.Service
}

func TestValidateResources(t *testing.T) {
	bad := []*api.Resources{
		{MemoryBytes: 1},
		{NanoCPUs: 42},
	}

	good := []*api.Resources{
		{MemoryBytes: 4096 * 1024 * 1024},
		{NanoCPUs: 1e9},
	}

	for _, b := range bad {
		err := validateResources(b)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	}

	for _, g := range good {
		assert.NoError(t, validateResources(g))
	}
}

func TestValidateResourceRequirements(t *testing.T) {
	bad := []*api.ResourceRequirements{
		{Limits: &api.Resources{MemoryBytes: 1}},
		{Reservations: &api.Resources{MemoryBytes: 1}},
	}
	good := []*api.ResourceRequirements{
		{Limits: &api.Resources{NanoCPUs: 1e9}},
		{Reservations: &api.Resources{NanoCPUs: 1e9}},
	}
	for _, b := range bad {
		err := validateResourceRequirements(b)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	}

	for _, g := range good {
		assert.NoError(t, validateResourceRequirements(g))
	}
}

func TestValidateTask(t *testing.T) {
	type badSource struct {
		s api.TaskSpec
		c codes.Code
	}

	for _, bad := range []badSource{
		{
			s: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: nil,
				},
			},
			c: codes.InvalidArgument,
		},
		// NOTE(stevvooe): can't actually test this case because we don't have
		// another runtime defined.
		// {
		//	s: &api.ServiceSpec{
		//		Template: &api.TaskSpec{
		//			Runtime:
		//		},
		//	},
		//	c: codes.Unimplemented,
		// },
		{
			s: createSpec("", "", 0).Task,
			c: codes.InvalidArgument,
		},
		{
			s: createSpec("", "busybox###", 0).Task,
			c: codes.InvalidArgument,
		},
		{
			s: createSpecWithDuplicateMounts("test").Task,
			c: codes.InvalidArgument,
		},
	} {
		err := validateTask(bad.s)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []api.TaskSpec{
		createSpec("", "image", 0).Task,
	} {
		err := validateTask(good)
		assert.NoError(t, err)
	}
}

func TestValidateServiceSpec(t *testing.T) {
	type BadServiceSpec struct {
		spec *api.ServiceSpec
		c    codes.Code
	}

	for _, bad := range []BadServiceSpec{
		{
			spec: nil,
			c:    codes.InvalidArgument,
		},
		{
			spec: &api.ServiceSpec{Annotations: api.Annotations{Name: "name"}},
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("name", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "image", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec(strings.Repeat("longname", 8), "image", 1),
			c:    codes.InvalidArgument,
		},
	} {
		err := validateServiceSpec(bad.spec)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err), grpc.ErrorDesc(err))
	}

	for _, good := range []*api.ServiceSpec{
		createSpec("name", "image", 1),
	} {
		err := validateServiceSpec(good)
		assert.NoError(t, err)
	}
}

func TestValidateRestartPolicy(t *testing.T) {
	bad := []*api.RestartPolicy{
		{
			Delay:  ptypes.DurationProto(time.Duration(-1 * time.Second)),
			Window: ptypes.DurationProto(time.Duration(-1 * time.Second)),
		},
		{
			Delay:  ptypes.DurationProto(time.Duration(20 * time.Second)),
			Window: ptypes.DurationProto(time.Duration(-4 * time.Second)),
		},
	}

	good := []*api.RestartPolicy{
		{
			Delay:  ptypes.DurationProto(time.Duration(10 * time.Second)),
			Window: ptypes.DurationProto(time.Duration(1 * time.Second)),
		},
	}

	for _, b := range bad {
		err := validateRestartPolicy(b)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	}

	for _, g := range good {
		assert.NoError(t, validateRestartPolicy(g))
	}
}

func TestValidateUpdate(t *testing.T) {
	bad := []*api.UpdateConfig{
		{Delay: duration.Duration{Seconds: -1, Nanos: 0}},
		{Delay: duration.Duration{Seconds: -1000, Nanos: 0}},
	}

	good := []*api.UpdateConfig{
		{Delay: duration.Duration{Seconds: 1, Nanos: 0}},
	}

	for _, b := range bad {
		err := validateUpdate(b)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	}

	for _, g := range good {
		assert.NoError(t, validateUpdate(g))
	}
}

func TestCreateService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	spec := createSpec("name", "image", 1)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	// test port conflicts
	spec = createSpec("name2", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 := createSpec("name3", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	// test no port conflicts when no publish port is specified
	spec3 := createSpec("name4", "image", 1)
	spec3.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec3})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)
	spec4 := createSpec("name5", "image", 1)
	spec4.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{TargetPort: uint32(9001), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec4})
	assert.NoError(t, err)

	// ensure no port conflict when different protocols are used
	spec = createSpec("name6", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9100), TargetPort: uint32(9100), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 = createSpec("name7", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9100), TargetPort: uint32(9100), Protocol: api.PortConfig_Protocol(api.ProtocolUDP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	assert.NoError(t, err)

	// test secret target conflicts
	spec = createSpecWithDuplicateSecretTargets("name8")
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	// test secret target conflicts with same secret and two references
	spec = createSpecWithDuplicateSecretTargets("name8")
	spec.Task.GetContainer().Secrets = []*api.SecretReference{
		{
			SecretName: "secret1",
			SecretID:   "secretID1",
			Mode:       api.SecretReference_FILE,
		},
		{
			SecretName: "secret2",
			SecretID:   "secretID2",
			Target:     "secret1",
			Mode:       api.SecretReference_FILE,
		},
	}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	// test two different secretReferences with using the same secret
	spec = createSpecWithDuplicateSecretTargets("name8")
	spec.Task.GetContainer().Secrets = []*api.SecretReference{
		{
			SecretName: "secret1",
			SecretID:   "secretID1",
			Mode:       api.SecretReference_FILE,
		},
		{
			SecretName: "secret1",
			SecretID:   "secretID1",
			Target:     "different_target",
			Mode:       api.SecretReference_FILE,
		},
	}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
}

func TestGetService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	service := createService(t, ts, "name", "image", 1)
	r, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	service.Meta.Version = r.Service.Meta.Version
	assert.Equal(t, service, r.Service)
}

func TestUpdateService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	service := createService(t, ts, "name", "image", 1)

	_, err := ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: "invalid", Spec: &service.Spec, ServiceVersion: &api.Version{}})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	// No update options.
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: &service.Spec})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: &service.Spec, ServiceVersion: &service.Meta.Version})
	assert.NoError(t, err)

	r, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	assert.Equal(t, service.Spec.Annotations.Name, r.Service.Spec.Annotations.Name)
	mode, ok := r.Service.Spec.GetMode().(*api.ServiceSpec_Replicated)
	assert.Equal(t, ok, true)
	assert.True(t, mode.Replicated.Replicas == 1)

	mode.Replicated.Replicas = 42
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: &r.Service.Meta.Version,
	})
	assert.NoError(t, err)

	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	assert.Equal(t, service.Spec.Annotations.Name, r.Service.Spec.Annotations.Name)
	mode, ok = r.Service.Spec.GetMode().(*api.ServiceSpec_Replicated)
	assert.Equal(t, ok, true)
	assert.True(t, mode.Replicated.Replicas == 42)

	// mode change not allowed
	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	r.Service.Spec.Mode = &api.ServiceSpec_Global{
		Global: &api.GlobalService{},
	}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: &r.Service.Meta.Version,
	})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), errModeChangeNotAllowed.Error()))

	// Versioning.
	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	version := &r.Service.Meta.Version

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	assert.NoError(t, err)

	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	assert.Error(t, err)

	// test port conflicts
	spec2 := createSpec("name2", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	assert.NoError(t, err)

	spec3 := createSpec("name3", "image", 1)
	rs, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec3})
	assert.NoError(t, err)

	spec3.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           spec3,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	spec3.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9001), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           spec3,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	assert.NoError(t, err)

	// test secret target conflicts
	spec4 := createSpecWithDuplicateSecretTargets("name8")
	spec5 := spec4.Copy()
	spec5.Task.GetContainer().Secrets = nil
	rs, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec5})
	assert.NoError(t, err)

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           spec4,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

}

// TODO(dongluochen): Network update is not supported yet and it's blocked
// from controlapi. This test should be removed once network update is supported.
func TestServiceUpdateRejectNetworkChange(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	spec := createSpec("name1", "image", 1)
	spec.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: "net20",
		},
	}
	cr, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)

	ur, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: cr.Service.ID})
	assert.NoError(t, err)
	service := ur.Service

	service.Spec.Networks[0].Target = "net30"

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &service.Spec,
		ServiceVersion: &service.Meta.Version,
	})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), errNetworkUpdateNotSupported.Error()))

	// Use TaskSpec.Networks
	spec = createSpec("name2", "image", 1)
	spec.Task.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: "net20",
		},
	}
	cr, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)

	ur, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: cr.Service.ID})
	assert.NoError(t, err)
	service = ur.Service

	service.Spec.Task.Networks[0].Target = "net30"

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &service.Spec,
		ServiceVersion: &service.Meta.Version,
	})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), errNetworkUpdateNotSupported.Error()))

	// Migrate networks from ServiceSpec.Networks to TaskSpec.Networks
	spec = createSpec("name3", "image", 1)
	spec.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: "net20",
		},
	}
	cr, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)

	ur, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: cr.Service.ID})
	assert.NoError(t, err)
	service = ur.Service

	service.Spec.Task.Networks = spec.Networks
	service.Spec.Networks = nil

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &service.Spec,
		ServiceVersion: &service.Meta.Version,
	})
	assert.NoError(t, err)
}

func TestRemoveService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.RemoveService(context.Background(), &api.RemoveServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	service := createService(t, ts, "name", "image", 1)
	r, err := ts.Client.RemoveService(context.Background(), &api.RemoveServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestValidateEndpointSpec(t *testing.T) {
	endPointSpec1 := &api.EndpointSpec{
		Mode: api.ResolutionModeDNSRoundRobin,
		Ports: []*api.PortConfig{
			{
				Name:       "http",
				TargetPort: 80,
			},
		},
	}

	endPointSpec2 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:          "http",
				TargetPort:    81,
				PublishedPort: 8001,
			},
			{
				Name:          "http",
				TargetPort:    80,
				PublishedPort: 8000,
			},
		},
	}

	// has duplicated published port, invalid
	endPointSpec3 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:          "http",
				TargetPort:    81,
				PublishedPort: 8001,
			},
			{
				Name:          "http",
				TargetPort:    80,
				PublishedPort: 8001,
			},
		},
	}

	// duplicated published port but different protocols, valid
	endPointSpec4 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:          "dns",
				TargetPort:    53,
				PublishedPort: 8002,
				Protocol:      api.ProtocolTCP,
			},
			{
				Name:          "dns",
				TargetPort:    53,
				PublishedPort: 8002,
				Protocol:      api.ProtocolUDP,
			},
		},
	}

	// multiple randomly assigned published ports
	endPointSpec5 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:       "http",
				TargetPort: 80,
				Protocol:   api.ProtocolTCP,
			},
			{
				Name:       "dns",
				TargetPort: 53,
				Protocol:   api.ProtocolUDP,
			},
			{
				Name:       "dns",
				TargetPort: 53,
				Protocol:   api.ProtocolTCP,
			},
		},
	}

	err := validateEndpointSpec(endPointSpec1)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	err = validateEndpointSpec(endPointSpec2)
	assert.NoError(t, err)

	err = validateEndpointSpec(endPointSpec3)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	err = validateEndpointSpec(endPointSpec4)
	assert.NoError(t, err)

	err = validateEndpointSpec(endPointSpec5)
	assert.NoError(t, err)
}

func TestServiceEndpointSpecUpdate(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	spec := &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: "name",
		},
		Task: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: "image",
				},
			},
		},
		Mode: &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: 1,
			},
		},
		Endpoint: &api.EndpointSpec{
			Ports: []*api.PortConfig{
				{
					Name:       "http",
					TargetPort: 80,
				},
			},
		},
	}

	r, err := ts.Client.CreateService(context.Background(),
		&api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotNil(t, r)

	// Update the service with duplicate ports
	spec.Endpoint.Ports = append(spec.Endpoint.Ports, &api.PortConfig{
		Name:       "fakehttp",
		TargetPort: 80,
	})
	_, err = ts.Client.UpdateService(context.Background(),
		&api.UpdateServiceRequest{Spec: spec})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
}

func TestListServices(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	r, err := ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Services)

	s1 := createService(t, ts, "name1", "image", 1)
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Services))

	createService(t, ts, "name2", "image", 1)
	createService(t, ts, "name3", "image", 1)

	// List all.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Services))

	// List with an ID prefix.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			IDPrefixes: []string{s1.ID[0:4]},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Services))
	assert.Equal(t, s1.ID, r.Services[0].ID)

	// List with simple filter.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name1"},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Services))

	// List with union filter.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name1", "name2"},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Services))

	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name1", "name2", "name4"},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Services))

	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name4"},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(r.Services))

	// List with filter intersection.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				NamePrefixes: []string{"name1"},
				IDPrefixes:   []string{s1.ID},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Services))

	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				NamePrefixes: []string{"name2"},
				IDPrefixes:   []string{s1.ID},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(r.Services))

	// List filter by label.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "yes",
				},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Services))

	// Value-less label.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "",
				},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Services))

	// Label intersection.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "",
					"unique": "name1",
				},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Services))

	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "",
					"unique": "error",
				},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(r.Services))
}
