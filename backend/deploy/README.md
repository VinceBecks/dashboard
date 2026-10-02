# Deployment

```sh
kubectl create namespace feature-dashboard
kubectl -n feature-dashboard create configmap feature-dashboard-config --from-file=config.yaml=../config.example.yaml
kubectl apply -f rbac.yaml -f deployment.yaml
```
