var PROTO_PATH = __dirname + '/warden.proto';

var grpc = require('grpc');
var proto = grpc.load(PROTO_PATH).warden;

/**
 * Implements the SayHello RPC method.
 */
function cluster(call) {
  call.on('data', function(data) {
    console.log('data: ', data);
  });
  call.on('end', function() {
    console.log('end');
    call.end();
  });
  call.write({
  	requestId: 'foo'
  });
}


/**
 * Starts an RPC server that receives requests for the Greeter service at the
 * sample server port
 */
function main() {
  var server = new grpc.Server();
  server.addProtoService(proto.ClusterAgentService.service, {cluster: cluster});
  server.bind('0.0.0.0:50051', grpc.ServerCredentials.createInsecure());
  server.start();
}

main();
