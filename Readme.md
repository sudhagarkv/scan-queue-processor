**Kafka docs:**

Kafka can be accessed by consumers via port 9092 on the following DNS name from within your cluster:

    kafka.trufflehog-scanner.svc.cluster.local

Each Kafka broker can be accessed by producers via port 9092 on the following DNS name(s) from within your cluster:

    kafka-controller-0.kafka-controller-headless.trufflehog-scanner.svc.cluster.local:9092
    kafka-controller-1.kafka-controller-headless.trufflehog-scanner.svc.cluster.local:9092
    kafka-controller-2.kafka-controller-headless.trufflehog-scanner.svc.cluster.local:9092

The CLIENT listener for Kafka client connections from within your cluster have been configured with the following security settings:
- SASL authentication

To connect a client to your Kafka, you need to create the 'client.properties' configuration files with the content below:
```
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-256
sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required \
    username="user1" \
    password="$(kubectl get secret kafka-user-passwords --namespace trufflehog-scanner -o jsonpath='{.data.client-passwords}' | base64 -d | cut -d , -f 1)";
```

To create a pod that you can use as a Kafka client run the following commands:

    kubectl run kafka-client --restart='Never' --image docker.io/bitnami/kafka:3.6.0-debian-11-r0 --namespace trufflehog-scanner --command -- sleep infinity
    kubectl cp --namespace trufflehog-scanner /path/to/client.properties kafka-client:/tmp/client.properties
    kubectl exec --tty -i kafka-client --namespace trufflehog-scanner -- bash

    PRODUCER:
        kafka-console-producer.sh \
            --producer.config /tmp/client.properties \
            --broker-list kafka-controller-0.kafka-controller-headless.trufflehog-scanner.svc.cluster.local:9092,kafka-controller-1.kafka-controller-headless.trufflehog-scanner.svc.cluster.local:9092,kafka-controller-2.kafka-controller-headless.trufflehog-scanner.svc.cluster.local:9092 \
            --topic test

    CONSUMER:
        kafka-console-consumer.sh \
            --consumer.config /tmp/client.properties \
            --bootstrap-server kafka.trufflehog-scanner.svc.cluster.local:9092 \
            --topic test \
            --from-beginning