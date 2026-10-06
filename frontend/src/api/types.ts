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

/** Konfigurierter Link; die URL ist vom Backend fertig gerendert. */
export interface Link {
  id: string;
  title: string;
  lucideIcon: string;
  lucideIconColor: string;
  url: string;
}

export type ObjectKind = 'namespace' | 'service' | 'ingress' | 'pod';

/** Referenz auf ein Objekt im Body von PATCH /api/actions/{action}. */
export interface ObjectRef {
  kind: ObjectKind;
  namespace: string;
  name: string;
}

/** Einordnung in die Hierarchie Projekt → Namespace → Service. */
export interface Parents {
  project: string;
  namespace?: string;
  service?: string;
}

/** Objekt-Typen im Event-Stream; node-port-service ist für Aktionen ein service. */
export type EventKind = ObjectKind | 'node-port-service';

/** Inhalt eines delete-Events; identifiziert ein Objekt über kind, parents und name. */
export interface ObjectHeader {
  kind: EventKind;
  name: string;
  parents: Parents;
}

interface ObjectBase extends ObjectHeader {
  actions: Action[];
  links: Link[];
}

export interface Container {
  name: string;
  image: string;
}

export interface PodObject extends ObjectBase {
  kind: 'pod';
  ready: boolean;
  containers: Container[];
}

/** Anwendungs-Service (ClusterIP). pods fehlt in upsert-Events. */
export interface ServiceObject extends ObjectBase {
  kind: 'service';
  /** Mindestens ein Replica ist ready. */
  running: boolean;
  /** IDs der Service-Bereiche (ProjectSnapshot.serviceAreas), zu denen der Service gehört. */
  areas: string[];
  pods?: PodObject[];
}

export interface IngressObject extends ObjectBase {
  kind: 'ingress';
  /** Fertig aufgelöste https-URLs der Rules. */
  urls: string[];
}

/** Service vom Typ NodePort. */
export interface NodePortServiceObject extends ObjectBase {
  kind: 'node-port-service';
  /** Fertige Adressen <dns-name>:<node-port>, je Port eine. */
  addresses: string[];
}

/** Feature-Umgebung. Die Kind-Listen fehlen in upsert-Events. */
export interface NamespaceObject extends ObjectBase {
  kind: 'namespace';
  services?: ServiceObject[];
  nodePortServices?: NodePortServiceObject[];
  ingresses?: IngressObject[];
}

export type DashboardObject = NamespaceObject | ServiceObject | PodObject | IngressObject | NodePortServiceObject;

/** Service-Bereich eines Projekts; im Dashboard ein Reiter der Namespace-Kachel. */
export interface ServiceArea {
  id: string;
  label: string;
}

export interface ProjectSnapshot {
  name: string;
  /** Bereiche in Config-Reihenfolge, „others“ immer zuletzt. */
  serviceAreas: ServiceArea[];
  namespaces: NamespaceObject[];
}

/** Inhalt des snapshot-Events (erstes Event auf GET /api/events). */
export interface Snapshot {
  projects: ProjectSnapshot[];
}

export interface ActionResult {
  success: boolean;
  message: string;
}

export type FormValue = string | number | string[];
