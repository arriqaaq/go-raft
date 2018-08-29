package raft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

var (

	// AppendEntriesPath is where the AppendEntries RPC handler (POST) will be
	// installed by the HTTPTransport.
	AppendEntriesPath = "/raft/appendentries"

	// RequestVotePath is where the requestVote RPC handler (POST) will be
	// installed by the HTTPTransport.
	RequestVotePath = "/raft/requestvote"
)

type Transporter interface {
	SendVoteRequest(server *Server, peer *Peer, req *RequestVoteRequest) *RequestVoteResponse
	SendAppendEntriesRequest(server *Server, peer *Peer, req *AppendEntriesRequest) *AppendEntriesResponse
}

type HTTPMuxer interface {
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}

func NewHTTPTransporter() *HTTPTransporter {
	t := &HTTPTransporter{
		appendEntriesPath: AppendEntriesPath,
		requestVotePath:   RequestVotePath,
		Transport:         &http.Transport{DisableKeepAlives: false},
	}
	t.httpClient.Transport = t.Transport
	return t
}

type HTTPTransporter struct {
	appendEntriesPath string
	requestVotePath   string
	httpClient        http.Client
	Transport         *http.Transport
}

// Retrieves the AppendEntries path.
func (t *HTTPTransporter) AppendEntriesPath() string {
	return t.appendEntriesPath
}

// Retrieves the RequestVote path.
func (t *HTTPTransporter) RequestVotePath() string {
	return t.requestVotePath
}

// Applies Raft routes to an HTTP router for a given server.
func (t *HTTPTransporter) Install(server *Server, mux HTTPMuxer) {
	mux.HandleFunc(t.AppendEntriesPath(), t.appendEntriesHandler(server))
	mux.HandleFunc(t.RequestVotePath(), t.requestVoteHandler(server))
}

//
func (t *HTTPTransporter) appendEntriesHandler(server *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		reqBody, _ := ioutil.ReadAll(r.Body)

		req := &AppendEntriesRequest{}
		if err := json.Unmarshal(reqBody, req); err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		log.Println(server.Name(), "/appendEntries", req.LeaderID)

		resp := server.AppendEntries(req)
		respBody, _ := json.Marshal(resp)
		if _, err := w.Write(respBody); err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}
}

// Handles incoming RequestVote requests.
func (t *HTTPTransporter) requestVoteHandler(server *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		reqBody, _ := ioutil.ReadAll(r.Body)

		req := &RequestVoteRequest{}
		if err := json.Unmarshal(reqBody, req); err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		log.Println(req.CandidateName, "/requestVote")

		resp := server.RequestVote(req)
		respBody, _ := json.Marshal(resp)
		if _, err := w.Write(respBody); err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}
}

// Sends a RequestVote RPC to a peer.
func (t *HTTPTransporter) SendVoteRequest(server *Server, peer *Peer, req *RequestVoteRequest) *RequestVoteResponse {
	var b bytes.Buffer
	reqBody, reqErr := json.Marshal(req)
	if reqErr != nil {
		log.Println("transporter.rv.encoding.error:", reqErr)
		return nil
	}
	b.Write(reqBody)

	url := fmt.Sprintf("%s%s", peer.connectionString, t.RequestVotePath())
	// log.Println("POST", url)

	httpResp, respErr := t.httpClient.Post(url, "application/json", &b)
	if httpResp == nil || respErr != nil {
		log.Println("transporter.rv.response.error:", respErr)
		return nil
	}
	defer httpResp.Body.Close()

	resp := &RequestVoteResponse{}
	respBody, _ := ioutil.ReadAll(httpResp.Body)

	if err := json.Unmarshal(respBody, resp); err != nil {
		log.Println("transporter.rv.decoding.error:", err)
		return nil
	}

	return resp
}

// Sends an AppendEntries RPC to a peer.
func (t *HTTPTransporter) SendAppendEntriesRequest(server *Server, peer *Peer, req *AppendEntriesRequest) *AppendEntriesResponse {
	var b bytes.Buffer

	reqBody, reqErr := json.Marshal(req)

	if reqErr != nil {
		log.Println("transporter.rv.encoding.error:", reqErr)
		return nil
	}
	b.Write(reqBody)

	url := fmt.Sprintf("%s%s", peer.connectionString, t.AppendEntriesPath())
	// log.Println("POST", url)

	httpResp, err := t.httpClient.Post(url, "application/json", &b)
	if httpResp == nil || err != nil {
		log.Println("transporter.ae.response.error:", err)
		return nil
	}
	defer httpResp.Body.Close()

	resp := &AppendEntriesResponse{}
	respBody, _ := ioutil.ReadAll(httpResp.Body)

	if err := json.Unmarshal(respBody, resp); err != nil {
		log.Println("transporter.ae.decoding.error:", err)
		return nil
	}
	// log.Println("response macha: ", string(respBody), url)

	return resp
}
