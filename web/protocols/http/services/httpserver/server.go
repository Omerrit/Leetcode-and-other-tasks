package httpserver

import (
	"context"
	"fmt"
	"gerrit-share.lan/go/actors"
	"gerrit-share.lan/go/actors/plugins/published"
	"gerrit-share.lan/go/actors/starter"
	"gerrit-share.lan/go/basicerrors"
	"gerrit-share.lan/go/debug"
	"gerrit-share.lan/go/errors"
	"gerrit-share.lan/go/inspect"
	"gerrit-share.lan/go/inspect/inspectable"
	"gerrit-share.lan/go/inspect/inspectables"
	"gerrit-share.lan/go/inspect/inspectwrappers"
	"gerrit-share.lan/go/inspect/json/fromjson"
	"gerrit-share.lan/go/inspect/json/tojson"
	"gerrit-share.lan/go/interfaces"
	"gerrit-share.lan/go/utils/actorwrappers"
	"gerrit-share.lan/go/utils/flags"
	"gerrit-share.lan/go/utils/maps"
	"gerrit-share.lan/go/web/protocols/http/services"
	"gerrit-share.lan/go/web/protocols/http/services/httpserver/internal/frombytes"
	"gerrit-share.lan/go/web/protocols/http/services/httpserver/internal/metadata"
	"gerrit-share.lan/go/web/protocols/http/services/httpserver/internal/replies"
	"gerrit-share.lan/go/web/protocols/http/services/httpserver/internal/tobytes"
	"gerrit-share.lan/go/web/protocols/http/services/httpserver/jsonrpc"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
)

const endpointsFileName = "/endpoints"

func NewHttpServer(hostPort flags.HostPort, name string, dir string) *httpServer {
	server := new(httpServer)
	server.name = name
	server.dir = dir
	server.server = http.Server{
		Addr:    hostPort.String(),
		Handler: server}
	return server
}

type httpServer struct {
	actors.Actor
	endpoints   HttpRestEndpoints
	server      http.Server
	serverActor actors.ActorService
	name        string
	dir         string
}

func (h *httpServer) MakeBehaviour() actors.Behaviour {
	log.Println(h.name, "started")
	var b actors.Behaviour
	var handle starter.Handle
	handle.Acquire(h, handle.DependOn, h.Quit)
	h.loadEndpoints()
	h.subscribeForActors()
	h.registerOwnEndpoints()
	b.Name = services.DefaultHttpServerName
	b.AddCommand(new(urlPath), h.generateDoc).ResultString()
	b.AddCommand(new(httpRequest), h.serveRequest).ResultByteString()
	b.AddCommand(new(editEndpoint), h.editEndpoint)
	b.AddCommand(new(getEndpoints), h.getEndpoints).Result(new(endpointInfoByName))
	b.AddCommand(new(editEndpointByName), h.editEndpointByName)
	h.SetPanicProcessor(h.onPanic)
	h.SetFinishedServiceProcessor(h.onServiceFinished)
	h.SetExitProcessor(h.onExit)
	h.serverActor = h.System().RunAsyncSimple(func() error {
		log.Println("listen and serve started")
		h.server.ListenAndServe()
		log.Println("listen and serve shutdown")
		return nil
	})
	h.Monitor(h.serverActor)
	return b
}

func (h *httpServer) registerOwnEndpoints() {
	h.processEndpointMessage(urlPathName, new(urlPath), inspectwrappers.NewStringValue(""), h.Service(), ".doc")
	h.processEndpointMessage(editEndpointName, new(editEndpoint), nil, h.Service(), "edit.api")
	h.processEndpointMessage(getEndpointsName, new(getEndpoints), new(endpointInfoByName), h.Service(), "get.api")
	h.processEndpointMessage(editEndpointByNameName, new(editEndpointByName), nil, h.Service(), "editbyname.api")
}

func (h *httpServer) subscribeForActors() {
	input := actors.NewSimpleCallbackStreamInput(func(data inspect.Inspectable) error {
		array := data.(*actors.ActorsArray)
		for _, a := range *array {
			actor := a
			h.Monitor(actor)
			h.SendRequest(actor, actors.GetInfo{},
				actors.OnReply(func(reply interface{}) {
					endpoints := reply.(*actors.ActorCommands)
					for _, command := range endpoints.Commands {
						h.processEndpointMessage(command.Command.TypeName(), command.Command.Sample(),
							command.Result.Sample(), actor, prepareEndpointName(command.Command.TypeName(), endpoints.Name))
					}
				}))
		}
		return nil
	}, func(base *actors.StreamInputBase) {
		base.RequestData(new(actors.ActorsArray), 10)
	})
	published.Subscribe(h, input, h.Quit)
}

func (h *httpServer) Shutdown() error {
	h.onExit()
	log.Println(h.name, "shut down")
	return nil
}

func (h *httpServer) onPanic(err errors.StackTraceError) {
	log.Println("panic:", err, err.StackTrace())
	h.onExit()
	h.Quit(err)
}

func (h *httpServer) onServiceFinished(service actors.ActorService, err error) {
	if service == h.serverActor {
		log.Println("http server finished with error:", err)
		h.Quit(err)
	}
	debug.Printf("service finished: %p\n", service)
	h.endpoints.RemoveByDestination(service)
}

func (h *httpServer) onExit() {
	err := h.saveEndpoints()
	if err != nil {
		log.Println("failed to save endpoints:", err)
	}
	err = h.server.Shutdown(context.Background())
	if err != nil {
		log.Println("error while shutdown:", err)
	}
}

func (h *httpServer) editEndpoint(cmd interface{}) (actors.Response, error) {
	cmdEdit := cmd.(*editEndpoint)
	err := h.endpoints.Edit(cmdEdit.oldResource, cmdEdit.oldMethod, cmdEdit.oldHttpMethod, cmdEdit.resource, cmdEdit.method, cmdEdit.httpMethod)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *httpServer) editEndpointByName(cmd interface{}) (actors.Response, error) {
	cmdEdit := cmd.(*editEndpointByName)
	err := h.endpoints.EditByName(cmdEdit.name, cmdEdit.resource, cmdEdit.method, cmdEdit.httpMethod)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *httpServer) getEndpoints(c interface{}) (actors.Response, error) {
	getCmd := c.(*getEndpoints)
	result := &endpointInfoByName{}
	for name, info := range h.endpoints.info {
		if getCmd.edited == info.changed {
			newInfo := &endpointInfo{changed: true}
			if getCmd.edited {
				newInfo.update(info.Resource, info.Method, info.HttpMethod)
			} else {
				parsed := ParseEndpoint(name)
				newInfo.update(parsed.Name, parsed.Method, defaultHttpMethod)
			}
			(*result)[name] = newInfo
		}
	}
	return result, nil
}

func (h *httpServer) loadEndpoints() {
	content, err := ioutil.ReadFile(path.Join(h.dir, endpointsFileName))
	if err != nil {
		log.Println("failed to load endpoints:", err)
	}
	reader := inspect.NewGenericInspector(fromjson.NewInspector(content, 0))
	h.endpoints.Inspect(reader)
}

func (h *httpServer) saveEndpoints() error {
	file, err := os.Create(path.Join(h.dir, endpointsFileName))
	if err != nil {
		return err
	}
	defer file.Close()
	inspector := &tojson.Inspector{}
	serializer := inspect.NewGenericInspector(inspector)
	h.endpoints.info.Inspect(serializer)
	if serializer.GetError() != nil {
		return serializer.GetError()
	}
	_, err = file.Write(inspector.Output())
	return nil
}

func (h *httpServer) processEndpointMessage(commandName string, commandSample inspect.Inspectable,
	resultSample inspect.Inspectable, service actors.ActorService, name string) {

	commandMetaData, err := metadata.MakeCommandMetaData(commandSample)
	if err != nil {
		log.Printf("failed to parse command sample: %v\nendpoint has been ignored by http-server\n", err)
		return
	}
	commandMetaData.Description = inspectables.GetDescription(commandName)

	if metadata.IsNested(commandMetaData) {
		log.Printf("nested command has been detected during registration of endpoint: %v\nendpoint has been ignored by http-server\n", commandName)
		return
	}

	result, err := MakeResultInfo(resultSample)
	if err != nil {
		log.Printf("failed to parse result sample: %v\nendpoint has been ignored by http-server\n", err)
		return
	}

	h.endpoints.Add(name, service, inspectables.Get(commandName), result, commandMetaData)
}

func (h *httpServer) getEndpointInfo(request *http.Request) ([]byte, endpointInfo, error) {
	var endpoint endpointInfo
	path, method, methods := h.endpoints.FindEndpointMethods(request.URL.Path, '/')
	debug.Println("path:", request.URL.Path)
	debug.Println(string(path), string(method))
	if methods == nil {
		return path, endpoint, ErrResourceNotFound
	}
	httpMethods, ok := methods[string(method)]
	if !ok {
		httpMethods, ok = methods[""]
		if !ok {
			return path, endpoint, ErrMethodNotAllowed
		}
		path = method
	}
	info, ok := httpMethods[strings.ToLower(request.Method)]
	if !ok || info.Dest == nil {
		return path, endpoint, ErrHttpMethodNotAllowed
	}
	endpoint = *info
	if endpoint.CommandMetaData.PathIndex == metadata.NoPath && len(path) > 0 {
		return path, endpoint, ErrPathNotAllowed
	}
	return path, endpoint, nil
}

func logFailedToSerializeErr(endpoint string, err error) {
	log.Printf("failed to serialize response from %v. %v", endpoint, err)
}

func (h *httpServer) processSingleCommand(command inspect.Inspectable, endpoint endpointInfo) actors.Response {
	promise := &replies.BytesPromise{}
	canceller := h.SendRequest(endpoint.Dest, command,
		actors.OnReply(func(reply interface{}) {
			result := inspectable.NewGenericValue(endpoint.ResultInfo.TypeId)
			result.SetValue(reply)
			bytes, err := tobytes.ToBytes(result)
			if err != nil {
				logFailedToSerializeErr(endpoint.OriginalName, err)
				promise.Fail(err)
				return
			}
			promise.Deliver(bytes)
		}).OnError(promise.Fail))
	promise.OnCancel(canceller.Cancel)
	return promise
}

func (h *httpServer) processCommandBatch(commands []inspect.Inspectable, endpoint endpointInfo) actors.Response {
	promise := &replies.BytesPromise{}
	promiseCounter := len(commands)
	result := make(groupResponse, len(commands))
	cancellers := make([]interfaces.Canceller, len(commands))
	deliver := func() {
		promiseCounter--
		if promiseCounter == 0 {
			writer := &tojson.Inspector{}
			serializer := inspect.NewGenericInspector(writer)
			result.Inspect(serializer)
			if serializer.GetError() != nil {
				logFailedToSerializeErr(endpoint.OriginalName, serializer.GetError())
				promise.Fail(serializer.GetError())
				return
			}
			promise.Deliver(writer.Output())
		}
	}
	for i, command := range commands {
		index := i
		result[index].Result = inspectable.NewGenericValue(endpoint.ResultInfo.TypeId)
		cancellers = append(cancellers, h.SendRequest(endpoint.Dest, command, actors.OnReply(func(reply interface{}) {
			result[index].Result.SetValue(reply)
			deliver()
		}).OnError(func(err error) {
			result[index].Err = err
			deliver()
		})))
	}
	promise.OnCancel(func() {
		for _, canceller := range cancellers {
			canceller.Cancel()
		}
	})
	return promise
}

func (h *httpServer) processJsonRequest(request *http.Request, endpoint endpointInfo, path []byte) (actors.Response, error) {
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	var acceptValues bool
	params := endpoint.CommandMetaData.UnderlyingValues
	if len(params) == 1 || len(params) == 2 && !(endpoint.CommandMetaData.PathIndex == metadata.NoPath) {
		acceptValues = true
	}

	inspector := frombytes.NewProxyInspector(path, body, 0, acceptValues)
	deserializer := inspect.NewGenericInspector(inspector)
	objectCommands := &commandBatch{generator: endpoint.CommandGenerator}
	objectCommands.Inspect(deserializer)
	if deserializer.GetError() != nil {
		return nil, deserializer.GetError()
	}
	if inspector.IsBatch {
		return h.processCommandBatch(objectCommands.commands, endpoint), nil
	}
	return h.processSingleCommand(objectCommands.commands[0], endpoint), nil
}

func (h *httpServer) processUnknownRequest(request *http.Request, endpoint endpointInfo, path []byte) (actors.Response, error) {
	bodyPart := make([]byte, 1)
	n, _ := request.Body.Read(bodyPart)
	if n > 0 {
		return nil, ErrNoContentType
	}
	command, err := commandFromPath(endpoint, path)
	if err != nil {
		return nil, err
	}
	return h.processSingleCommand(command, endpoint), nil
}

func (h *httpServer) processRequest(request *http.Request, endpoint endpointInfo, path []byte) (actors.Response, error) {
	if request.Method == http.MethodGet {
		command, err := commandFromQueryString(request, endpoint, path)
		if err != nil {
			return nil, err
		}
		return h.processSingleCommand(command, endpoint), nil
	}
	switch contentType := request.Header.Get("Content-Type"); contentType {
	case "application/x-www-form-urlencoded":
		command, err := commandFromQueryString(request, endpoint, path)
		if err != nil {
			return nil, err
		}
		return h.processSingleCommand(command, endpoint), nil
	case "application/json":
		return h.processJsonRequest(request, endpoint, path)
	default:
		if strings.HasPrefix(contentType, "multipart/form-data") {
			command, err := commandFromFormData(request, endpoint, path)
			if err != nil {
				return nil, err
			}
			return h.processSingleCommand(command, endpoint), nil
		}
		if len(contentType) > 0 {
			return nil, ErrUnsupportedContentType
		}
		return h.processUnknownRequest(request, endpoint, path)
	}
}

func (h *httpServer) processRpcCommand(command requestWithDestination) (actors.Response, error) {
	if command.err != nil {
		response := jsonrpc.NewResponse(command.responseType)
		response.Result.Err = command.err
		bytes, err := tobytes.ToBytes(response)
		if err != nil {
			return nil, err
		}
		return replies.BytesReply(bytes), nil
	}
	promise := &replies.BytesPromise{}
	response := jsonrpc.NewResponse(command.responseType)
	writer := &tojson.Inspector{}
	serializer := inspect.NewGenericInspector(writer)
	deliver := func() {
		response.Inspect(serializer)
		if serializer.GetError() != nil {
			logFailedToSerializeErr(command.request.Method, serializer.GetError())
			promise.Fail(jsonrpc.Describe(serializer.GetError(), jsonrpc.ErrInternalError))
			return
		}
		promise.Deliver(writer.Output())
	}
	canceller := h.SendRequest(command.destination, command.request.Params, actors.OnReply(func(reply interface{}) {
		response.Result.Result.SetValue(reply)
		deliver()
	}).OnError(func(err error) {
		response.Result.Err = jsonrpc.Describe(err, jsonrpc.ErrInvalidParams)
		deliver()
	}))
	promise.OnCancel(canceller.Cancel)
	return promise, nil
}

func (h *httpServer) processRpcCommandBatch(commands []requestWithDestination) (actors.Response, error) {
	promise := &replies.BytesPromise{}
	var validCommands int
	for _, command := range commands {
		if command.err == nil {
			validCommands++
		}
	}
	cancellers := make([]interfaces.Canceller, validCommands)
	result := make(jsonrpc.ResponseBatch, len(commands))
	deliver := func() {
		validCommands--
		if validCommands == 0 {
			output, err := tobytes.ToBytes(&result)
			if err != nil {
				promise.Fail(jsonrpc.Describe(err, jsonrpc.ErrInternalError))
				return
			}
			promise.Deliver(output)
		}
	}
	for i, command := range commands {
		index := i
		response := jsonrpc.NewResponse(command.responseType)
		if command.err != nil {
			response.Result.Err = command.err
			result[index] = response
			continue
		}
		cancellers = append(cancellers, h.SendRequest(command.destination, command.request.Params, actors.OnReply(func(reply interface{}) {
			response.Result.Result.SetValue(reply)
			result[index] = response
			deliver()
		}).OnError(func(err error) {
			response.Result.Err = jsonrpc.Describe(err, jsonrpc.ErrInvalidParams)
			result[index] = response
			deliver()
		})))
	}
	if validCommands == 0 {
		output, err := tobytes.ToBytes(&result)
		if err != nil {
			return nil, jsonrpc.Describe(err, jsonrpc.ErrInvalidParams)
		}
		return replies.BytesReply(output), nil
	}
	promise.OnCancel(func() {
		for _, canceller := range cancellers {
			canceller.Cancel()
		}
	})
	return promise, nil
}

func (h *httpServer) processJsonRpcRequest(request *http.Request) (actors.Response, error) {
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, jsonrpc.Describe(err, jsonrpc.ErrInternalError)
	}

	inspector := frombytes.NewProxyBatchInspector(body, 0)
	deserializer := inspect.NewGenericInspector(inspector)
	objectCommands := &rpcRequestBatch{endpoints: &h.endpoints}
	objectCommands.Inspect(deserializer)
	if deserializer.GetError() != nil {
		return nil, deserializer.GetError()
	}
	if inspector.IsBatch {
		return h.processRpcCommandBatch(objectCommands.data)
	}
	return h.processRpcCommand(objectCommands.data[0])
}

func (h *httpServer) serveRequest(cmd interface{}) (actors.Response, error) {
	requestCmd := cmd.(*httpRequest)
	if requestCmd.Header.Get("Content-Type") == "application/json-rpc" {
		return h.processJsonRpcRequest((*http.Request)(requestCmd))
	}
	path, endpoint, err := h.getEndpointInfo((*http.Request)(requestCmd))
	if err != nil {
		return nil, err
	}
	return h.processRequest((*http.Request)(requestCmd), endpoint, path)
}

func (h *httpServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Add("Access-Control-Allow-Origin", "*")
	var cancelled bool
	h.System().Become(actorwrappers.NewShutdownableActor(request.Context().Done(),
		func() {
			writer.WriteHeader(http.StatusBadGateway)
			cancelled = true
		},
		func(actor *actors.Actor) actors.Behaviour {
			var b actors.Behaviour
			actor.SendRequest(h.Service(), (*httpRequest)(request),
				actors.OnReply(func(reply interface{}) {
					writer.Write(reply.([]byte))
				}).OnError(func(err error) {
					if !cancelled {
						processError(writer, err)
					}
				}))
			return b
		}))
}

func (h *httpServer) generateLinks(prefix string) maps.String {
	var depth int
	if prefix != "/" {
		tempEndpointSplitter := new(SplitEndpoint)
		tempEndpointSplitter.SetEndpoint(prefix, '/')
		depth = tempEndpointSplitter.NumParts()
	}
	prefix = strings.TrimLeft(prefix, "/")
	var links maps.String
	if depth >= len(h.endpoints.groupEndpoints) {
		return nil
	}
	for key := range h.endpoints.groupEndpoints[depth] {
		if strings.HasPrefix(key, prefix) && key != prefix {
			key = strings.Replace(key, ".", "/", -1)
			//parts := strings.Split(urlPathName, ".")
			//links.Add("/"+key, fmt.Sprintf("http://%v/%v/%v/%v", h.server.Addr, services.DefaultHttpServerName, key, parts[len(parts) - 1]))
			links.Add("/"+key, fmt.Sprintf("http://%v/doc/%v/", h.server.Addr, key))
		}
	}
	return links
}

func (h *httpServer) generateMethods(url string) (map[string]map[string]endpointInfo, error) {
	data := make(map[string]map[string]endpointInfo)
	path, method, methods := h.endpoints.FindEndpointMethods(url, '/')
	if len(path) > 0 {
		return nil, basicerrors.BadParameter
	}
	data[url] = make(map[string]endpointInfo)
	if len(method) > 0 {
		return nil, basicerrors.NotFound
	}
	for methodName, endpoints := range methods {
		for httpMethodName, endpoint := range endpoints {
			if endpoint.Dest != nil {
				data[url][fmt.Sprintf("[%v] %v", httpMethodName, methodName)] = *endpoint
			}
		}
	}
	return data, nil
}

func (h *httpServer) generateDoc(cmd interface{}) (actors.Response, error) {
	pathCmd := cmd.(*urlPath)
	realUrl := strings.Replace(pathCmd.id, ".", "/", -1)
	realUrl = "/" + realUrl
	methods, err := h.generateMethods(realUrl)
	if err != nil {
		return replies.StringReply(""), err
	}

	links := h.generateLinks(realUrl)

	result := new(strings.Builder)
	err = executeMethodsTemplate(result, methods)
	if err != nil {
		return replies.StringReply(""), err
	}
	if links != nil {
		err = executeLinksTemplate(result, links)
		if err != nil {
			return replies.StringReply(""), err
		}
	}
	return replies.StringReply(result.String()), nil
}

func init() {
	starter.SetCreator(services.DefaultHttpServerName, func(s *actors.Actor, name string) (actors.ActorService, error) {
		server := NewHttpServer(services.DefaultHttpServerParams(), services.DefaultHttpServerName, services.DefaultDir())
		return s.System().Spawn(server), nil
	})
}