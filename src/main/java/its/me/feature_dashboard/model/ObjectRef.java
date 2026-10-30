package its.me.feature_dashboard.model;

/** Verweis auf ein Objekt; bei Namespaces sind namespace und name gleich. */
public record ObjectRef(Kind kind, String namespace, String name) {

	public static ObjectRef namespace(String name) {
		return new ObjectRef(Kind.NAMESPACE, name, name);
	}

	@Override
	public String toString() {
		return kind + " " + namespace + "/" + name;
	}
}
