package its.me.feature_dashboard.pipelines;

/** Fragt den Status einer Pipeline bei GitLab ab (z. B. "running", "success"). */
public interface StatusChecker {

	/**
	 * Status der Pipeline.
	 * @throws PipelineNotFoundException wenn GitLab die Pipeline nicht kennt
	 */
	String pipelineStatus(long id) throws Exception;

	/** GitLab kennt die Pipeline nicht (404). */
	class PipelineNotFoundException extends Exception {
		public PipelineNotFoundException(String message) {
			super(message);
		}
	}
}
