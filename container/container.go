package container

import (
	"fmt"
	"strings"

	"github.com/samalba/dockerclient"
)

// Docker labels from container metadata
const (
	LabelTugbot      = "gaiadocker.tugbot"
	LabelTest        = "tugbot.test"
	LabelCreatedFrom = "tugbot.created.from"
	LabelStopSignal  = "tugbot.stop-signal"
	LabelZodiac      = "tugbot.zodiac.original-image"
)

// NewContainer returns a new Container instance instantiated with the
// specified ContainerInfo and ImageInfo structs.
func NewContainer(containerInfo *dockerclient.ContainerInfo, imageInfo *dockerclient.ImageInfo) *Container {
	return &Container{
		containerInfo: containerInfo,
		imageInfo:     imageInfo,
	}
}

// Container represents a running Docker container.
type Container struct {
	Stale bool

	containerInfo *dockerclient.ContainerInfo
	imageInfo     *dockerclient.ImageInfo
}

// ID returns the Docker container ID.
func (c Container) ID() string {
	return c.containerInfo.Id
}

// Name returns the Docker container name.
func (c Container) Name() string {
	return strings.TrimPrefix(c.containerInfo.Name, "/")
}

// ImageID returns the ID of the Docker image that was used to start the
// container.
func (c Container) ImageID() string {
	return c.imageInfo.Id
}

// ImageName returns the name of the Docker image that was used to start the
// container. If the original image was specified without a particular tag, the
// "latest" tag is assumed.
func (c Container) ImageName() string {
	// Compatibility w/ Zodiac deployments
	imageName, ok := c.containerInfo.Config.Labels[LabelZodiac]
	if !ok {
		imageName = c.containerInfo.Config.Image
	}

	if !strings.Contains(imageName, ":") {
		imageName = fmt.Sprintf("%s:latest", imageName)
	}

	return imageName
}

// Links returns a list containing the names of all the containers to which
// this container is linked.
func (c Container) Links() []string {
	var links []string

	if (c.containerInfo != nil) && (c.containerInfo.HostConfig != nil) {
		for _, link := range c.containerInfo.HostConfig.Links {
			name := strings.Split(link, ":")[0]
			links = append(links, name)
		}
	}

	return links
}

// IsTugbot returns whether or not the current container is the tugbot container itself.
// The tugbot container is identified by the presence of the "gaiadocker.tugbot" label in
// the container metadata.
func (c Container) IsTugbot() bool {
	val, ok := c.containerInfo.Config.Labels[LabelTugbot]
	return ok && val == "true"
}

// StopSignal returns the custom stop signal (if any) that is encoded in the
// container's metadata. If the container has not specified a custom stop
// signal, the empty string "" is returned.
func (c Container) StopSignal() string {
	if val, ok := c.containerInfo.Config.Labels[LabelStopSignal]; ok {
		return val
	}

	return ""
}

// Ideally, we'd just be able to take the ContainerConfig from the old container
// and use it as the starting point for creating the new container; however,
// the ContainerConfig that comes back from the Inspect call merges the default
// configuration (the stuff specified in the metadata for the image itself)
// with the overridden configuration (the stuff that you might specify as part
// of the "docker run"). In order to avoid unintentionally overriding the
// defaults in the new image we need to separate the override options from the
// default options. To do this we have to compare the ContainerConfig for the
// running container with the ContainerConfig from the image that container was
// started from. This function returns a ContainerConfig which contains just
// the options overridden at runtime.
func (c Container) runtimeConfig() *dockerclient.ContainerConfig {
	config := c.containerInfo.Config
	imageConfig := c.imageInfo.Config

	if config.WorkingDir == imageConfig.WorkingDir {
		config.WorkingDir = ""
	}

	if config.User == imageConfig.User {
		config.User = ""
	}

	if sliceEqual(config.Cmd, imageConfig.Cmd) {
		config.Cmd = nil
	}

	if sliceEqual(config.Entrypoint, imageConfig.Entrypoint) {
		config.Entrypoint = nil
	}

	config.Env = sliceSubtract(config.Env, imageConfig.Env)

	config.Labels = stringMapSubtract(config.Labels, imageConfig.Labels)

	config.Volumes = structMapSubtract(config.Volumes, imageConfig.Volumes)

	config.ExposedPorts = structMapSubtract(config.ExposedPorts, imageConfig.ExposedPorts)
	for p := range c.containerInfo.HostConfig.PortBindings {
		config.ExposedPorts[p] = struct{}{}
	}

	config.Image = c.ImageName()
	return config
}

// Any links in the HostConfig need to be re-written before they can be
// re-submitted to the Docker create API.
func (c Container) hostConfig() *dockerclient.HostConfig {
	hostConfig := c.containerInfo.HostConfig

	for i, link := range hostConfig.Links {
		name := link[0:strings.Index(link, ":")]
		alias := link[strings.LastIndex(link, "/"):]

		hostConfig.Links[i] = fmt.Sprintf("%s:%s", name, alias)
	}

	return hostConfig
}

// IsTugbotCandidate returns whether or not the current container is a candidate to run by tugbot.
// A candidate container is identified by the presence of "tugbot.test",
// it doesn't contain "tugbot.created.from" in the container metadata and it state is "Exited".
func (c Container) IsTugbotCandidate() bool {
	ret := false
	val, ok := c.containerInfo.Config.Labels[LabelTest]
	if ok && val == "true" {
		val, ok = c.containerInfo.Config.Labels[LabelCreatedFrom]
		if !ok || len(val) == 0 {
			ret = c.containerInfo.State.StateString() == "exited"
		}
	}

	return ret
}
