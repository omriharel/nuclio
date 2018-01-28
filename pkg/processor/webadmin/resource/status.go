/*
Copyright 2017 The Nuclio Authors.

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

package resource

import (
	"net/http"

	"github.com/nuclio/nuclio/pkg/processor/webadmin"
	"github.com/nuclio/nuclio/pkg/restful"
)

type statusResource struct {
	*resource
}

func (sr *statusResource) GetSingle(request *http.Request) (string, restful.Attributes) {
	return "processor", restful.Attributes{
		"oper_status": sr.getProcessor().GetStatus().String(),
	}
}

// register the resource
var status = &statusResource{
	resource: newResource("status", []restful.ResourceMethod{
		restful.ResourceMethodGetList,
	}),
}

func init() {
	status.Resource = status
	status.Register(webadmin.WebAdminResourceRegistrySingleton)
}