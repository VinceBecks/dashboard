// Typen entsprechen backend/internal/api/openapi.yaml.

export type FieldType = 'text' | 'select' | 'multi-select' | 'number';

export interface FormFieldOption {
  value: string;
  label: string;
}

export interface FormField {
  id: string;
  label: string;
  type: FieldType;
  isRequired: boolean;
  description?: string;
  options?: FormFieldOption[];
}

export interface Action {
  id: string;
  title: string;
  lucideIcon: string;
  lucideIconColor: string;
  formFields?: FormField[];
}

export type ObjectKind = 'namespace' | 'service' | 'ingress' | 'pod';

export interface ObjectRef {
  kind: ObjectKind;
  namespace: string;
  name: string;
}

interface ObjectBase {
  name: string;
  namespace?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  actions: Action[];
}

export interface Namespace extends ObjectBase {
  project: string;
}

export interface ContainerPort {
  name?: string;
  containerPort: number;
  protocol?: string;
}

export interface Container {
  name: string;
  image: string;
  ports?: ContainerPort[];
}

export interface Pod extends ObjectBase {
  phase: string;
  ready: boolean;
  containers: Container[];
}

export interface ServicePort {
  name?: string;
  protocol?: string;
  port: number;
  targetPort: string;
  resolvedTargetPort?: number;
  nodePort?: number;
}

export interface Service extends ObjectBase {
  type: string;
  selector?: Record<string, string>;
  ports: ServicePort[];
  replicas: string[];
}

export interface IngressBackend {
  serviceName: string;
  servicePortName?: string;
  servicePortNumber?: number;
  resolvedServicePortNumber?: number;
}

export interface IngressPath {
  pathType: string;
  path: string;
  backend: IngressBackend;
}

export interface IngressRule {
  host: string;
  paths: IngressPath[];
}

export interface Ingress extends ObjectBase {
  rules: IngressRule[];
}

export interface Project {
  name: string;
  namespaces: Namespace[];
}

export interface ProjectsResponse {
  projects: Project[];
}

export interface NamespaceResponse {
  namespace: Namespace;
  services: Service[];
  ingresses: Ingress[];
  pods: Pod[];
}

export interface ActionResult {
  success: boolean;
  message: string;
}

export type FormValue = string | number | string[];
