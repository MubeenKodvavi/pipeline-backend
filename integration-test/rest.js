import http from "k6/http";

import {
  sleep,
  check,
  group,
  fail
} from "k6";
import {
  FormData
} from "https://jslib.k6.io/formdata/0.0.2/index.js";
import {
  randomString
} from "https://jslib.k6.io/k6-utils/1.1.0/index.js";

import {
  pipelinePublicHost,
  connectorPublicHost,
  modelPublicHost
} from "./const.js";

import {
  genHeader
} from "./helper.js"

import * as constant from "./const.js";
import * as pipelinePublic from './rest-pipeline-public.js';
import * as pipelinePublicWithJwt from './rest-pipeline-public-with-jwt.js';
import * as pipelinePrivate from './rest-pipeline-private.js';
import * as triggerSync from './rest-trigger-sync.js';
import * as triggerAsync from './rest-trigger-async.js';

export let options = {
  setupTimeout: '300s',
  insecureSkipTLSVerify: true,
  thresholds: {
    checks: ["rate == 1.0"],
  },
};

export function setup() {

  group("Connector Backend API: Create a http source connector", function () {

    var res = http.request("POST", `${connectorPublicHost}/v1alpha/source-connectors`,
      JSON.stringify({
        "id": "source-http",
        "source_connector_definition": "source-connector-definitions/source-http",
        "connector": {
          "configuration": {}
        }
      }), constant.params)
    check(res, {
      "POST /v1alpha/source-connectors response status for creating HTTP source connector 201": (r) => r.status === 201,
    })

  });

  group("Connector Backend API: Create a http destination connector", function () {

    var res = http.request("POST", `${connectorPublicHost}/v1alpha/destination-connectors`,
      JSON.stringify({
        "id": "destination-http",
        "destination_connector_definition": "destination-connector-definitions/destination-http",
        "connector": {
          "configuration": {}
        }
      }), constant.params)

    check(res, {
      "POST /v1alpha/destination-connectors response status for creating HTTP destination connector 201": (r) => r.status === 201,
    })

  });

  group("Connector Backend API: Create a gRPC source connector", function () {

    var res = http.request("POST", `${connectorPublicHost}/v1alpha/source-connectors`,
      JSON.stringify({
        "id": "source-grpc",
        "source_connector_definition": "source-connector-definitions/source-grpc",
        "connector": {
          "configuration": {}
        }
      }), constant.params)
    check(res, {
      "POST /v1alpha/source-connectors response status for creating gRPC source connector 201": (r) => r.status === 201,
    })

  });

  group("Connector Backend API: Create a gRPC destination connector", function () {

    var res = http.request("POST", `${connectorPublicHost}/v1alpha/destination-connectors`,
      JSON.stringify({
        "id": "destination-grpc",
        "destination_connector_definition": "destination-connector-definitions/destination-grpc",
        "connector": {
          "configuration": {}
        }
      }), constant.params)

    check(res, {
      "POST /v1alpha/destination-connectors response status for creating gRPC destination connector 201": (r) => r.status === 201,
    })

  });

  group("Connector Backend API: Create a CSV destination connector 1", function () {

    var res = http.request("POST", `${connectorPublicHost}/v1alpha/destination-connectors`,
      JSON.stringify({
        "id": constant.dstCSVConnID1,
        "destination_connector_definition": "destination-connector-definitions/airbyte-destination-csv",
        "connector": {
          "configuration": {
            "destination_path": "/local/pipeline-backend-test-1"
          }
        }
      }), constant.params)

    check(res, {
      "POST /v1alpha/destination-connectors response status for creating CSV destination connector 201": (r) => r.status === 201,
    })

  });

  group("Connector Backend API: Create a CSV destination connector 2", function () {

    var res = http.request("POST", `${connectorPublicHost}/v1alpha/destination-connectors`,
      JSON.stringify({
        "id": constant.dstCSVConnID2,
        "destination_connector_definition": "destination-connector-definitions/airbyte-destination-csv",
        "connector": {
          "configuration": {
            "destination_path": "/local/pipeline-backend-test-2"
          }
        }
      }), constant.params)

    check(res, {
      "POST /v1alpha/destination-connectors response status for creating CSV destination connector 201": (r) => r.status === 201,
    })

  });

  group("Model Backend API: Deploy a detection model", function () {
    let fd = new FormData();
    let model_description = randomString(20)
    fd.append("id", constant.model_id);
    fd.append("description", model_description);
    fd.append("model_definition", constant.model_def_name);
    fd.append("content", http.file(constant.det_model, "dummy-det-model.zip"));
    let createClsModelRes = http.request("POST", `${modelPublicHost}/v1alpha/models/multipart`, fd.body(), {
      headers: {
        "Content-Type": `multipart/form-data; boundary=${fd.boundary}`
      },
    })
    check(createClsModelRes, {
      "POST /v1alpha/models/multipart task det response status": (r) => r.status === 201
    });

    // Check model creation finished
    let currentTime = new Date().getTime();
    let timeoutTime = new Date().getTime() + 120000;
    while (timeoutTime > currentTime) {
      var res = http.get(`${modelPublicHost}/v1alpha/${createClsModelRes.json().operation.name}`, {
          headers: genHeader(`application/json`),
      })
      if (res.json().operation.done === true) {
          break
      }
      sleep(1)
      currentTime = new Date().getTime();
  }


    var res = http.post(`${modelPublicHost}/v1alpha/models/${constant.model_id}/deploy`, {}, constant.params)

    check(res, {
      [`POST /v1alpha/models/${constant.model_id}/deploy online task det response status`]: (r) => r.status === 200
    });

    // Check the model state being updated in 120 secs (in integration test, model is dummy model without download time but in real use case, time will be longer)
    currentTime = new Date().getTime();
    timeoutTime = new Date().getTime() + 120000;
    while (timeoutTime > currentTime) {
      let res = http.get(`${modelPublicHost}/v1alpha/models/${constant.model_id}/watch`, {
        headers: genHeader(`application/json`),
      })
      if (res.json().state === "STATE_ONLINE") {
        break
      }
      sleep(1)
      currentTime = new Date().getTime();
    }

  });

}

export default function (data) {

  /*
   * Pipelines API - API CALLS
   */

  // Health check
  {
    group("Pipelines API: Health check", () => {
      check(http.request("GET", `${pipelinePublicHost}/v1alpha/health/pipeline`), {
        "GET /health/pipeline response status is 200": (r) => r.status === 200,
      });
    });
  }

  if (!constant.apiGatewayMode) {
    pipelinePrivate.CheckList()
    pipelinePrivate.CheckLookUp()

    pipelinePublicWithJwt.CheckCreate()
    pipelinePublicWithJwt.CheckList()
    pipelinePublicWithJwt.CheckGet()
    pipelinePublicWithJwt.CheckUpdate()
    pipelinePublicWithJwt.CheckUpdateState()
    pipelinePublicWithJwt.CheckRename()
    pipelinePublicWithJwt.CheckLookUp()
  }

  pipelinePublic.CheckCreate()
  pipelinePublic.CheckList()
  pipelinePublic.CheckGet()
  pipelinePublic.CheckUpdate()
  pipelinePublic.CheckUpdateState()
  pipelinePublic.CheckRename()
  pipelinePublic.CheckLookUp()
  pipelinePublic.CheckWatch()

  triggerSync.CheckTriggerSyncSingleImageSingleModel()
  triggerSync.CheckTriggerSyncMultiImageSingleModel()
  // Don't support this temporarily
  // triggerSync.CheckTriggerSyncMultiImageMultiModel()

  triggerAsync.CheckTriggerAsyncSingleImageSingleModel()
  triggerAsync.CheckTriggerAsyncMultiImageSingleModel()

  // Don't support this temporarily
  // triggerAsync.CheckTriggerAsyncMultiImageMultiModel()
  // triggerAsync.CheckTriggerAsyncMultiImageMultiModelMultipleDestination()

}

export function teardown(data) {

  group("Connector API: Delete all pipelines created by this test", () => {
    for (const pipeline of http.request("GET", `${pipelinePublicHost}/v1alpha/pipelines?page_size=100`).json("pipelines")) {
      check(http.request("DELETE", `${pipelinePublicHost}/v1alpha/pipelines/${pipeline.id}`), {
        [`DELETE /v1alpha/pipelines response status is 204`]: (r) => r.status === 204,
      });
    }
  });

  group("Connector Backend API: Delete the http source connector", function () {
    check(http.request("DELETE", `${connectorPublicHost}/v1alpha/source-connectors/source-http`), {
      [`DELETE /v1alpha/source-connectors/source-http response status 204`]: (r) => r.status === 204,
    });
  });

  group("Connector Backend API: Delete the http destination connector", function () {
    check(http.request("DELETE", `${connectorPublicHost}/v1alpha/destination-connectors/destination-http`), {
      [`DELETE /v1alpha/destination-connectors/destination-http response status 204`]: (r) => r.status === 204,
    });
  });

  group("Connector Backend API: Delete the gRPC source connector", function () {
    check(http.request("DELETE", `${connectorPublicHost}/v1alpha/source-connectors/source-grpc`), {
      [`DELETE /v1alpha/source-connectors/source-grpc response status 204`]: (r) => r.status === 204,
    });
  });

  group("Connector Backend API: Delete the gRPC destination connector", function () {
    check(http.request("DELETE", `${connectorPublicHost}/v1alpha/destination-connectors/destination-grpc`), {
      [`DELETE /v1alpha/destination-connectors/destination-grpc response status 204`]: (r) => r.status === 204,
    });
  });

  group("Connector Backend API: Delete the csv destination connector", function () {
    check(http.request("DELETE", `${connectorPublicHost}/v1alpha/destination-connectors/${constant.dstCSVConnID1}`), {
      [`DELETE /v1alpha/destination-connectors/${constant.dstCSVConnID1} response status 204`]: (r) => r.status === 204,
    });
  });
  group("Connector Backend API: Delete the csv destination connector", function () {
    check(http.request("DELETE", `${connectorPublicHost}/v1alpha/destination-connectors/${constant.dstCSVConnID2}`), {
      [`DELETE /v1alpha/destination-connectors/${constant.dstCSVConnID2} response status 204`]: (r) => r.status === 204,
    });
  });

  group("Model Backend API: Delete the detection model", function () {
    check(http.request("DELETE", `${modelPublicHost}/v1alpha/models/${constant.model_id}`, null, constant.params), {
      [`DELETE /v1alpha/models/${constant.model_id} response status is 204`]: (r) => r.status === 204,
    });
  });

}
