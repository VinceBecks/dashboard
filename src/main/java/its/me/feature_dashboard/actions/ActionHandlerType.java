package its.me.feature_dashboard.actions;

/**
 * Handler-Typ (Wert von {@code handler:} bzw. {@code action-name:}). Jede Implementierung ist eine Spring-Bean; je
 * aktivierter Aktion entsteht über {@link #create} eine eigene Handler-Instanz.
 */
public interface ActionHandlerType {

	String type();

	ActionHandler create(HandlerDeps deps);
}
