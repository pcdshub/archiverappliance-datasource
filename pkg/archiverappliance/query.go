package archiverappliance

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/sasaki77/archiverappliance-datasource/pkg/functions"
	"github.com/sasaki77/archiverappliance-datasource/pkg/models"
)

func Query(ctx context.Context, c client, req *backend.QueryDataRequest) *backend.QueryDataResponse {
	// create response struct
	response := backend.NewQueryDataResponse()
	responsePipe := make(chan models.QueryMgr)

	for _, q := range req.Queries {
		go func(ctx context.Context, q backend.DataQuery, client client, responsePipe chan models.QueryMgr) {
			res := backend.DataResponse{}
			qm, err := models.ReadQueryModel(q)

			if err != nil {
				res.Error = err
			} else {
				res = singleQuery(ctx, qm, c)
			}

			responsePipe <- models.QueryMgr{
				Res:    res,
				QRefID: q.RefID,
			}
		}(ctx, q, c, responsePipe)
	}

	timeoutDurationSeconds := 30 // units are seconds
	timeoutDuration, _ := time.ParseDuration(strconv.Itoa(timeoutDurationSeconds) + "s")
	timeoutPipe := time.After(timeoutDuration)

queryCollector:
	for range req.Queries {
		// save the response in a hashmap
		// based on with RefID as identifier
		select {
		case rtn := <-responsePipe:
			response.Responses[rtn.QRefID] = rtn.Res
		case <-timeoutPipe:
			log.DefaultLogger.Warn("Timeout limit for QueryData has been reached")
			break queryCollector
		}
	}

	return response
}

type queryResponse struct {
	response models.SingleData
	err      error
}

func singleQuery(ctx context.Context, qm models.ArchiverQueryModel, client client) backend.DataResponse {

	targetPvList := makeTargetPVList(client, qm.Target, qm.Regex, qm.MaxNumPVs)

	// execute the individual queries
	responseData := make([]*models.SingleData, 0, len(targetPvList))
	responsePipe := make(chan queryResponse)

	// Create timeout. If any request routines take longer than timeoutDurationSeconds to execute, they will be dropped.
	timeoutDurationSeconds := 30 // units are seconds
	timeoutDuration, _ := time.ParseDuration(strconv.Itoa(timeoutDurationSeconds) + "s")
	timeoutPipe := time.After(timeoutDuration)

	// create goroutines for individual requests
	for _, targetPv := range targetPvList {
		go func(targetPv string, pipe chan queryResponse) {
			parsedResponse, err := client.ExecuteSingleQuery(targetPv, qm)
			pipe <- queryResponse{response: parsedResponse, err: err}
		}(targetPv, responsePipe)
	}

	// Collect responses from the request goroutines
	var responseErr error
responseCollector:
	for range targetPvList {
		select {
		case response := <-responsePipe:
			if response.err != nil {
				if responseErr == nil {
					responseErr = response.err
				}
				continue
			}
			responseData = append(responseData, &response.response)
		case <-timeoutPipe:
			log.DefaultLogger.Warn("Timeout limit for query has been reached")
			break responseCollector
		}
	}

	// Apply Alias to the data
	var aliasErr error
	responseData, aliasErr = applyAlias(responseData, qm)
	if aliasErr != nil {
		log.DefaultLogger.Warn("Error applying alias")
	}

	sort.Slice(responseData, func(i, j int) bool { return responseData[i].Name < responseData[j].Name })

	// Apply Functions to the data
	var funcErr error
	responseData, funcErr = functions.ApplyFunctions(responseData, qm)
	if funcErr != nil {
		log.DefaultLogger.Warn("Error applying functions")
	}

	// Extrapolate data as necessary
	for idx, data := range responseData {
		responseData[idx] = dataExtrapol(data, qm)
	}

	response := backend.DataResponse{}

	// for each query response, compile the data into response.Frames
	for _, singleResponse := range responseData {
		frame := singleResponse.ToFrame(qm.FormatOption)

		// add the frames to the response
		response.Frames = append(response.Frames, frame)
	}

	response.Error = responseErr

	return response
}

func applyAlias(sD []*models.SingleData, qm models.ArchiverQueryModel) ([]*models.SingleData, error) {
	// Alias is not set. Return data as is is.
	if qm.Alias == "" {
		return sD, nil
	}

	var rep *regexp.Regexp
	if qm.AliasPattern != "" {
		var err error
		rep, err = regexp.Compile(qm.AliasPattern)
		if err != nil {
			return sD, err
		}
	}

	for _, d := range sD {
		d.ApplyAlias(qm.Alias, rep)
	}

	return sD, nil
}

func dataExtrapol(singleResponse *models.SingleData, qm models.ArchiverQueryModel) *models.SingleData {
	if qm.Interval >= 1 || qm.Operator == "last" || qm.DisableExtrapol || qm.BackendQuery {
		return singleResponse
	}

	newResponse := singleResponse.Extrapolation(qm.TimeRange.To)

	return newResponse
}

func makeTargetPVList(client client, target string, regex bool, maxNum int) []string {
	// PV name isolation for syntax like "(PV:NAME:1|PV:NAME:2|...)" is always required even if regex is enabled.
	// That's because AA sever doesn't support full regular expression.
	isolatedPvList := isolateBasicQuery(target)

	var targetPvList []string
	if regex {
		// If the user is using a regex to specify the PVs, parse and resolve the regex expression first
		// assemble the list of PVs to be queried for
		var regexPvList []string
		for _, v := range isolatedPvList {
			pvs, _ := client.FetchRegexTargetPVs(v, maxNum)
			regexPvList = append(regexPvList, pvs...)
		}
		targetPvList = regexPvList
	} else {
		targetPvList = isolatedPvList
	}

	// Each name in list should be unique
	var uniqPVList []string
	m := map[string]bool{}
	for _, pvname := range targetPvList {
		if !m[pvname] {
			m[pvname] = true
			uniqPVList = append(uniqPVList, pvname)
		}

	}

	return uniqPVList
}
