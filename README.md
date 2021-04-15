[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new?template=https%3A%2F%2Fgithub.com%2Fmorgangallant%2Fscheduler%2Ftree%2Ftrunk&plugins=postgresql&envs=ENDPOINT%2CSECRET&ENDPOINTDesc=Request+Endpoint&SECRETDesc=openssl+rand+-hex+32&ENDPOINTDefault=https%3A%2F%2Fexample.com%2Fsched)

A simple job scheduler backed by Postgres used in production at https://operand.ai. Setup needs two
environment variables, `SECRET` and `ENDPOINT`. The secret is attached to incoming/outgoing requests
within the `Scheduler-Secret` HTTP header, and is used both by the scheduler to verify that incoming
request is legit, as well as the end-application to verify the request is coming from the scheduler.
The endpoint is simply the URL to send HTTP requests to.

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

Cancelling a Scheduled job
Note: If the job doesn't exist, this will fail silently.

```
POST https://scheduler.yourcompany.com/delete
Headers: Scheduler-Secret=XXXXXX

{
    "id":"cknjdu2k300153zugmucamxxo"
}

Response: None
```

And that's it! Feel free to file issues or do pull requests.
