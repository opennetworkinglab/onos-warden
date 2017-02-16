var AWS = require('aws-sdk');
AWS.config.update({region:'us-west-1'});
var ec2 = new AWS.EC2();

var PROTO_PATH = __dirname + '/agent.proto';

var grpc = require('grpc');
var stub = grpc.load(PROTO_PATH).warden;
var client = new stub.ClusterAgentService('localhost:50051', grpc.credentials.createInsecure());

var call = client.cluster();
call.on('end', function() {
	//TODO we want to re-establish connection on close
	console.log('end');
});
call.on('error', function(err) {
	//TODO we want to re-establish connection on err
	console.log('Error', err);
});
call.on('status', function(status) {
	console.log('status', status);
});
console.log(call);


//FIXME add name/tag
var instanceParams = {
  InstanceCount: 1, 
  LaunchSpecification: {
   ImageId: "ami-8d8c78c9", 
   InstanceType: "m3.medium", //m3.2xlarge
   KeyName: "onos-test",
   SecurityGroupIds: [
      "all open"
   ],
   BlockDeviceMappings: [
      {
        DeviceName: '/dev/sda1',
        Ebs: {
          DeleteOnTermination: true,
          Encrypted: false,
          VolumeSize: 16,
          VolumeType: 'gp2'
        }
      }
    ]
  }, 
  SpotPrice: "1", //FIXME
  Type: "one-time"
 };

// Map of Spot Instance requests
var instances = {};

call.on('data', function(data) {
	//FIXME this should do some reservations stuff
	// borrow and return
	console.log('Got message: ', data);
});

function deleteInstance(spotId) {
	if (!(spotId in instances)) {
		return;
	}
	console.log('deleting: ' + spotId)
	delete instances[spotId];
	// Withdrawn the cluster
	call.write({
		clusterId: spotId,
  		state: 'UNAVAILABLE',
  	});
}

function addOrUpdateInstance(spotId, data) {
	var prev = instances[spotId];
	var prevState = prev ? prev['State'] : null;
	var state = data['State'];
	// Update the spot request in the map
	instances[spotId] = data;
    if (state == 'active' && prevState != 'active') {
    	//FIXME allocate from requestQueue
    	call.write({
			clusterId: spotId,
	  		state: 'AVAILABLE',
	  		headNodeIP: 'foo' //FIXME grab this from the instance
  		});
    }
}

function saveInstances(data) {
	data['SpotInstanceRequests'].forEach(function(req) {
		var spotId = req['SpotInstanceRequestId'];
		var state = req['State'];
		console.log(spotId + ': ' + state);
		console.log(req);
		if (state == 'cancelled' || state == 'closed') {
			deleteInstance(spotId)
		} else {
			addOrUpdateInstance(spotId, req);
		}

   	});
}

function updateInstances(forceAll = false) {
	var spotIds = Object.keys(instances)
	var params;
	if (forceAll) {
		params = {};
	} else if (spotIds.length == 0) {
		return;
	} else {
		params = {
			SpotInstanceRequestIds: spotIds
		};
	}
	ec2.describeSpotInstanceRequests(params, function(err, data) {
		if (err) {
			console.log(err, err.stack); // an error occurred
			return;
		}
		saveInstances(data);
	});
}

function requestInstance() {
	ec2.requestSpotInstances(instanceParams, function(err, data) {
		if (err) {
		   console.log(err, err.stack); // an error occurred
		   return;
		}
	   	saveInstances(data);
	});
}

function terminateInstances(spotIds) {
	if (spotIds.length == 0) {
		return;
	}

	var instanceIds = [];
	spotIds.forEach(function (id) {
		var instanceId = instances[id]['InstanceId']
		if (instanceId) {
		    instanceIds.push(instanceId)
		}
	});

	if (instanceIds.length > 0) {
		var params = {
		  InstanceIds: instanceIds
		};
		ec2.terminateInstances(params, function(err, data) {
		  if (err) console.log(err, err.stack); // an error occurred
		  else     console.log(data);           // successful response
		});
	}

	var params = {
		SpotInstanceRequestIds: spotIds
	};
	ec2.cancelSpotInstanceRequests(params, function(err, data) {
		if (err) {
	   		console.log(err, err.stack); // an error occurred
	   		return;
	   	}
	   	console.log(data);           // successful response
	   	data['CancelledSpotInstanceRequests'].forEach(function(req) {
	   		var state = req['State']
	   		if (state == 'closed' || state == 'cancelled') {
	   			deleteInstance(req['SpotInstanceRequestId']);
	   		} else {
	   			//FIXME retry the cancel...
	   		}
	   	});
	 });
}

/// FIXME ---- kick this off after grpc session established
// Module Initialization
//FIXME: Verify that this code only runs once
updateInstances(true);
setInterval(function() {
	console.log(new Date());
	updateInstances();

	// TODO: check expiring instances and cancel
}, 10 * 1000 /* 10 seconds */);


//FIXME this will get triggered on request
requestInstance();

//FIXME comment this out
setTimeout(function() {
	terminateInstances(Object.keys(instances));
}, 60*1000);



// var params = {
//   Resources: [
//      "ami-78a54011"
//   ], 
//   Tags: [
//      {
//     Key: "Stack", 
//     Value: "production"
//    }
//   ]
//  };
//  ec2.createTags(params, function(err, data) {
//    if (err) console.log(err, err.stack); // an error occurred
//    else     console.log(data);           // successful response
//  });

 //FIXME shutdown hook to cancel all instances


/*
{ SpotInstanceRequestId: 'sir-qrr8dzxk',
  SpotPrice: '0.770000',
  Type: 'one-time',
  State: 'open',
  Status: 
   { Code: 'pending-evaluation',
     UpdateTime: 2017-01-23T22:51:57.000Z,
     Message: 'Your Spot request has been submitted for review, and is pending evaluation.' },
  LaunchSpecification: 
   { ImageId: 'ami-8d8c78c9',
     KeyName: 'onos-test',
     SecurityGroups: [ [Object] ],
     InstanceType: 'm3.medium',
     BlockDeviceMappings: [ [Object] ],
     NetworkInterfaces: [],
     Monitoring: { Enabled: false } },
  CreateTime: 2017-01-23T22:51:56.000Z,
  ProductDescription: 'Linux/UNIX',
  Tags: [] }


{ SpotInstanceRequestId: 'sir-asagdjvg',
  SpotPrice: '0.770000',
  Type: 'one-time',
  State: 'open',
  Status: 
   { Code: 'pending-fulfillment',
     UpdateTime: 2017-01-23T22:55:33.000Z,
     Message: 'Your Spot request is pending fulfillment.' },
  LaunchSpecification: 
   { ImageId: 'ami-8d8c78c9',
     KeyName: 'onos-test',
     SecurityGroups: [ [Object] ],
     InstanceType: 'm3.medium',
     BlockDeviceMappings: [ [Object] ],
     NetworkInterfaces: [],
     Monitoring: { Enabled: false } },
  CreateTime: 2017-01-23T22:55:30.000Z,
  ProductDescription: 'Linux/UNIX',
  Tags: [],
  LaunchedAvailabilityZone: 'us-west-1a' }


{ SpotInstanceRequestId: 'sir-qrr8dzxk',
  SpotPrice: '0.770000',
  Type: 'one-time',
  State: 'active',
  Status: 
   { Code: 'fulfilled',
     UpdateTime: 2017-01-23T22:52:03.000Z,
     Message: 'Your Spot request is fulfilled.' },
  LaunchSpecification: 
   { ImageId: 'ami-8d8c78c9',
     KeyName: 'onos-test',
     SecurityGroups: [ [Object] ],
     InstanceType: 'm3.medium',
     BlockDeviceMappings: [ [Object] ],
     NetworkInterfaces: [],
     Monitoring: { Enabled: false } },
  InstanceId: 'i-0e0972e40cb465889',
  CreateTime: 2017-01-23T22:51:56.000Z,
  ProductDescription: 'Linux/UNIX',
  Tags: [],
  LaunchedAvailabilityZone: 'us-west-1a' }

{ SpotInstanceRequestId: 'sir-v7vgen7j',
  SpotPrice: '0.770000',
  Type: 'one-time',
  State: 'closed',
  Status: 
   { Code: 'instance-terminated-by-user',
     UpdateTime: 2017-01-23T22:58:11.000Z,
     Message: 'Spot Instance terminated due to user-initiated termination.' },
  LaunchSpecification: 
   { ImageId: 'ami-8d8c78c9',
     KeyName: 'onos-test',
     SecurityGroups: [ [Object] ],
     InstanceType: 'm3.medium',
     BlockDeviceMappings: [ [Object] ],
     NetworkInterfaces: [],
     Monitoring: { Enabled: false } },
  InstanceId: 'i-049fb8d6b63befc7b',
  CreateTime: 2017-01-23T22:56:51.000Z,
  ProductDescription: 'Linux/UNIX',
  Tags: [],
  LaunchedAvailabilityZone: 'us-west-1a' }
*/

