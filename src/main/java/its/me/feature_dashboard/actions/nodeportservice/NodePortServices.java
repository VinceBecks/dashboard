package its.me.feature_dashboard.actions.nodeportservice;

import java.util.Map;

/** Gemeinsame Konstanten der NodePort-Aktionen. */
public final class NodePortServices {

	/** action-name des Handlers, der NodePort-Services erstellt. */
	public static final String CREATE_ACTION_NAME = "node-port-service-on-service-base";
	/** action-name des Handlers, der sie wieder löscht. */
	public static final String DELETE_ACTION_NAME = "delete-node-port-service";
	/** Wird an den Namen des Basis-Services angehängt. */
	public static final String NAME_SUFFIX = "-node-port";
	/** Feld-ID der Port-Auswahl im Formular. */
	public static final String FIELD_PORTS = "ports";
	/** Markiert Services, die eine Dashboard-Aktion erstellt hat; Wert ist der action-name. */
	public static final String LABEL_CREATED_BY_ACTION = "feature-dashboard/created-by-action";
	/** Name des ClusterIP-Services, auf dem der NodePort-Service basiert. */
	public static final String ANNOTATION_BASE_SERVICE = "feature-dashboard/base-service";
	static final String LABEL_MANAGED_BY = "app.kubernetes.io/managed-by";
	static final String MANAGED_BY_VALUE = "feature-dashboard";

	private NodePortServices() {
	}

	/** Nur NodePort-Services mit dem Label dieser Aktion darf die Lösch-Aktion löschen. */
	static boolean createdByAction(String serviceType, Map<String, String> labels) {
		return "NodePort".equals(serviceType) && CREATE_ACTION_NAME.equals(labels == null ? null : labels.get(LABEL_CREATED_BY_ACTION));
	}
}
