package its.me.feature_dashboard.api;

/** Meldet, ob der Informer-Cache synchronisiert ist. */
@FunctionalInterface
public interface ReadinessCheck {
	boolean ready();
}
