package rpc

import "net/http"

// OpenAPIJSON documents the ConnectRPC JSON endpoints. It is intentionally
// checked in beside the handlers so a browser client has a discoverable,
// versioned contract even though ConnectRPC itself is protobuf-first.
const OpenAPIJSON = `{
  "openapi":"3.1.0",
  "info":{"title":"Assay local API","version":"v1","description":"ConnectRPC JSON API served by the local Assay binary. Streaming methods use Connect server streaming; Swagger UI documents their request and event payloads but cannot execute their streams."},
  "servers":[{"url":"/"}],
  "paths":{
    "/assay.v1.DataService/ListInstruments":{"post":{"tags":["Data"],"operationId":"listInstruments","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Empty"}}}},"responses":{"200":{"description":"Available instruments"}}}},
    "/assay.v1.DataService/DataStatus":{"post":{"tags":["Data"],"operationId":"dataStatus","description":"Cache-only readiness check for an exact symbol, timeframe, and date window.","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/EnsureDataRequest"}}}},"responses":{"200":{"description":"Local data readiness","content":{"application/json":{"schema":{"$ref":"#/components/schemas/DataStatusResponse"}}}}}}},
    "/assay.v1.DataService/EnsureData":{"post":{"tags":["Data"],"operationId":"ensureData","description":"Server stream of preparation progress.","x-connect-streaming":"server","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/EnsureDataRequest"}}}},"responses":{"200":{"description":"Stream of Progress events"}}}},
    "/assay.v1.PrimitiveService/ListPrimitives":{"post":{"tags":["Primitives"],"operationId":"listPrimitives","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Empty"}}}},"responses":{"200":{"description":"Registered primitive definitions"}}}},
    "/assay.v1.PrimitiveService/Evaluate":{"post":{"tags":["Primitives"],"operationId":"evaluatePrimitive","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/EvaluatePrimitiveRequest"}}}},"responses":{"200":{"description":"Primitive evaluation result"}}}},
    "/assay.v1.StudyService/Validate":{"post":{"tags":["Studies"],"operationId":"validateStudy","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ValidateRequest"}}}},"responses":{"200":{"description":"Validation errors and strategy hash"}}}},
    "/assay.v1.StudyService/Run":{"post":{"tags":["Studies"],"operationId":"runStudy","description":"Server stream: progress, equity chunks, trade count, then exactly one summary.","x-connect-streaming":"server","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/RunRequest"}}}},"responses":{"200":{"description":"Stream of RunUpdate events"}}}},
    "/assay.v1.RunService/ListRuns":{"post":{"tags":["Runs"],"operationId":"listRuns","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Empty"}}}},"responses":{"200":{"description":"Persisted run IDs"}}}},
    "/assay.v1.RunService/GetRun":{"post":{"tags":["Runs"],"operationId":"getRun","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/GetRunRequest"}}}},"responses":{"200":{"description":"Stored run summary"}}}},
    "/assay.v1.RunService/Compare":{"post":{"tags":["Runs"],"operationId":"compareRuns","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/CompareRunsRequest"}}}},"responses":{"200":{"description":"Parameter and bounded metric deltas"}}}}
  },
  "components":{"schemas":{
    "Empty":{"type":"object","additionalProperties":false},
    "EnsureDataRequest":{"type":"object","required":["symbol","timeframe","fromMs","toMs"],"properties":{"symbol":{"type":"string","example":"BINANCE:BTCUSDT"},"timeframe":{"type":"string","example":"m15"},"fromMs":{"type":"string","format":"int64","example":"1735689600000"},"toMs":{"type":"string","format":"int64","example":"1738368000000"}}},
    "DataStatusResponse":{"type":"object","properties":{"readiness":{"type":"string","enum":["DATA_READINESS_READY","DATA_READINESS_PARTIAL","DATA_READINESS_MISSING","DATA_READINESS_SOURCE_UNAVAILABLE","DATA_READINESS_CORRUPT","DATA_READINESS_UNSUPPORTED"]},"requestedFromMs":{"type":"string","format":"int64"},"requestedToMs":{"type":"string","format":"int64"},"cachedFromMs":{"type":"string","format":"int64"},"cachedToMs":{"type":"string","format":"int64"},"missingPartitions":{"type":"array","items":{"$ref":"#/components/schemas/DataPartition"}},"snapshot":{"type":"string"},"sources":{"type":"array","items":{"type":"string"}},"lastSuccessfulPreparationMs":{"type":"string","format":"int64"},"externalAccessRequired":{"type":"boolean"},"errorCode":{"type":"string"},"error":{"type":"string"}}},
    "DataPartition":{"type":"object","properties":{"symbol":{"type":"string"},"timeframe":{"type":"string"},"month":{"type":"string","example":"2026-08"},"fromMs":{"type":"string","format":"int64"},"toMs":{"type":"string","format":"int64"}}},
    "EvaluatePrimitiveRequest":{"type":"object","properties":{"pack":{"type":"string"},"type":{"type":"string"},"paramsJson":{"type":"string","format":"byte"},"snapshot":{"type":"string"}}},
    "ValidateRequest":{"type":"object","required":["strategyJson"],"properties":{"strategyJson":{"type":"string","format":"byte","description":"UTF-8 JSON strategy document, base64-encoded for protobuf JSON."}}},
    "RunRequest":{"type":"object","required":["strategyJson","fromMs","toMs"],"properties":{"strategyJson":{"type":"string","format":"byte"},"snapshot":{"type":"string"},"fromMs":{"type":"string","format":"int64"},"toMs":{"type":"string","format":"int64"}}},
    "GetRunRequest":{"type":"object","required":["hash"],"properties":{"hash":{"type":"string","description":"Run ID returned by RunSummary.runId"}}},
    "CompareRunsRequest":{"type":"object","required":["left","right"],"properties":{"left":{"type":"string"},"right":{"type":"string"}}}
  }}
}`

func openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	_, _ = w.Write([]byte(OpenAPIJSON))
}

func swaggerHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><title>Assay API</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/openapi.json',dom_id:'#swagger-ui',deepLinking:true})</script></body></html>`))
}
