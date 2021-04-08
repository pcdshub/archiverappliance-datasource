package main

import (
    "context"
    "encoding/json"
    "net/http"
    "time"
    "strconv"

    "github.com/grafana/grafana-plugin-sdk-go/backend"
    "github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
    "github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
    "github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

func newArchiverDataSource() datasource.ServeOpts {
    // Create a new instance manager
    log.DefaultLogger.Debug("Starting newArchiverDataSource")

    im := datasource.NewInstanceManager(newArchiverDataSourceInstance)
    ds := &ArchiverDatasource{
        im: im,
    }

    return datasource.ServeOpts{
        QueryDataHandler:   ds,
        CheckHealthHandler: ds,
    }
}

type ArchiverDatasource struct {
    // Structure defined by grafana-plugin-sdk-go. Implements QueryData and CheckHealth.
    im instancemgmt.InstanceManager
}

type QueryMgr struct {
    Res backend.DataResponse
    QRefID string
}

func (td *ArchiverDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
    // Structure defined by grafana-plugin-sdk-go. QueryData should unpack the req argument into individual queries.

    // create response struct
    response := backend.NewQueryDataResponse()

    responsePipe := make(chan QueryMgr)

    for _, q := range req.Queries {
        go func (ctx context.Context, q backend.DataQuery, req *backend.QueryDataRequest, responsePipe chan QueryMgr) {
            res := td.query(ctx, q, req.PluginContext)
            responsePipe <-QueryMgr{
                Res: res,
                QRefID: q.RefID,
            }
        }(ctx, q, req, responsePipe)
    }

    timeoutDurationSeconds := 30 // units are seconds
    timeoutDuration, _ := time.ParseDuration(strconv.Itoa(timeoutDurationSeconds)+"s")
    timeoutPipe := time.After(timeoutDuration)

    queryCollector:for range req.Queries {
        // save the response in a hashmap
        // based on with RefID as identifier
        select {
            case rtn := <-responsePipe:
                response.Responses[rtn.QRefID] = rtn.Res
            case <- timeoutPipe:
                log.DefaultLogger.Warn("Timeout limit for QueryData has been reached")
                break queryCollector
        }
    }
    return response, nil
}

func (td *ArchiverDatasource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
    // the frontend health check is still used at this time
    var status = backend.HealthStatusOk
    var message = "This is a fake success message"

    return &backend.CheckHealthResult{
        Status:     status,
        Message:    message,
    }, nil
}

func (td *ArchiverDatasource) query(ctx context.Context, query backend.DataQuery, pluginctx backend.PluginContext) backend.DataResponse {
    // Unmarshal the json into our queryModel
    var qm ArchiverQueryModel

    response := backend.DataResponse{}

    response.Error = json.Unmarshal(query.JSON, &qm)
    if response.Error != nil {
        return response
    }

    // make the query and compile the results into a SingleData instance
    targetPvList := make([]string,0)
    if qm.Regex {
        // If the user is using a regex to specify the PVs, parse and resolve the regex expression first

        // assemble the list of PVs to be queried for
        regexUrl := BuildRegexUrl(qm.Target, pluginctx)
        regexQueryResponse, _ := ArchiverRegexQuery(regexUrl)
        targetPvList, _ = ArchiverRegexQueryParser(regexQueryResponse)
    } else {
        // If a regex is not being used, only check for listed PVs
        targetPvList = IsolateBasicQuery(qm.Target)
    }

    // execute the individual queries
    responseData := make([]SingleData, 0, len(targetPvList))
    responsePipe := make(chan SingleData)

    // Create timeout. If any request routines take longer than timeoutDurationSeconds to execute, they will be dropped.
    timeoutDurationSeconds := 30 // units are seconds
    timeoutDuration, _ := time.ParseDuration(strconv.Itoa(timeoutDurationSeconds)+"s")
    timeoutPipe := time.After(timeoutDuration)

    // create goroutines for individual requests 
    for _, targetPv := range targetPvList {
        go func(targetPv string, pipe chan SingleData) {
            parsedResponse, _ := ExecuteSingleQuery(targetPv, query, pluginctx, qm)
            pipe <- parsedResponse
        }(targetPv, responsePipe)
    }

    // Collect responses from the request goroutines 
    responseCollector:for range targetPvList {
        select {
            case response := <-responsePipe:
                responseData = append(responseData, response)
            case <-timeoutPipe:
                log.DefaultLogger.Warn("Timeout limit for query has been reached")
                break responseCollector
        }
    }

    // Apply Functions to the data
    var funcErr error
    responseData, funcErr = ApplyFunctions(responseData, qm)
    if funcErr != nil {
        log.DefaultLogger.Warn("Error applying functions")
    }

    // Extrapolate data as necessary
    for idx, data := range responseData {
        responseData[idx] = DataExtrapol(data, qm, query)
    }

    // for each query response, compile the data into response.Frames
    for _, singleResponse := range responseData {

        frame := FrameBuilder(singleResponse)

        // add the frames to the response
        response.Frames = append(response.Frames, frame)
    }

    return response
}

type archiverInstanceSettings struct {
	httpClient *http.Client
}

func newArchiverDataSourceInstance(setting backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
    // Adheres to structure defined by grafana-plugin-sdk-go
    return &archiverInstanceSettings{
		httpClient: &http.Client{},
	}, nil
}

