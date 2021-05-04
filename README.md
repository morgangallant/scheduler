[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new?template=https%3A%2F%2Fgithub.com%2Fmorgangallant%2Fscheduler%2Ftree%2Ftrunk&plugins=postgresql&envs=ENDPOINT%2CSECRET&ENDPOINTDesc=Request+Endpoint&SECRETDesc=openssl+rand+-hex+32&ENDPOINTDefault=https%3A%2F%2Fexample.com%2Fsched)

A simple job scheduler backed by Postgres used in production at https://operand.ai. Setup needs two
environment variables, `SECRET` and `ENDPOINT`. The secret is attached to incoming/outgoing requests
within the `Scheduler-Secret` HTTP header, and is used both by the scheduler to verify that incoming
request is legit, as well as the end-application to verify the request is coming from the scheduler.
The endpoint is simply the URL to send HTTP requests to.

This scheduler also has support for CRON-type expressions. This allows the application to specify
a set of id's to run on a schedule. You should probably just make a request on application start
with all the IDs and the relevant CRON expressions. This will tear-down the world and re-start the
CRON server with the new values. This is how you should do it, especially in auto-deployment environments.
When a cron job fires, the application will get a POST with the "cron\_id" set. You should check for this
in the message and respond appropriately.


Example Usage:

Scheduling a Job

```
POST https://scheduler.yourcompany.com/insert
Headers: Scheduler-Secret=XXXXXX

{
    "timestamp": "2021-04-15T14:17:00-07:00",
    "body": {
        "foo": "bar"
    }
}

Response:
{"id":"cknjdu2k300153zugmucamxxo"}
```

Cancelling a Scheduled Job
(note: if the job doesn't exist, this will fail silently.)

```
POST https://scheduler.yourcompany.com/delete
Headers: Scheduler-Secret=XXXXXX

{
    "id":"cknjdu2k300153zugmucamxxo"
}

Response: None
```

Configuring CRON Jobs

```
POST https://scheduler.yourcompany.com/cron
Headers: Scheduler-Secret=XXXXXX

{
    "jobs":[
        {
            "id":"hello_world",
            "spec": "30 * * * * *"
        },
        {
            "id":"hello_world_2",
            "spec": "0 * * * * *"
        }
    ]
}

Response: None
```

And that's it! Feel free to file issues or do pull requests.
