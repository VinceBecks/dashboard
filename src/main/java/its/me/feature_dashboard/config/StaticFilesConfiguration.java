package its.me.feature_dashboard.config;

import java.io.IOException;

import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.io.FileSystemResource;
import org.springframework.core.io.Resource;
import org.springframework.web.servlet.config.annotation.ResourceHandlerRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;
import org.springframework.web.servlet.resource.PathResourceResolver;

/**
 * Liefert optional statische Dateien aus {@code dashboard.static-dir} aus (entspricht --static-dir des Go-Backends).
 * Unbekannte Pfade außerhalb von /api/ erhalten die index.html (SPA-Fallback).
 */
@Configuration(proxyBeanMethods = false)
@ConditionalOnProperty(prefix = "dashboard", name = "static-dir")
public class StaticFilesConfiguration implements WebMvcConfigurer {

	private final DashboardProperties properties;

	public StaticFilesConfiguration(DashboardProperties properties) {
		this.properties = properties;
	}

	@Override
	public void addResourceHandlers(ResourceHandlerRegistry registry) {
		String dir = properties.staticDir().endsWith("/") ? properties.staticDir() : properties.staticDir() + "/";
		registry.addResourceHandler("/**")
			.addResourceLocations(new FileSystemResource(dir))
			.resourceChain(false)
			.addResolver(new PathResourceResolver() {
				@Override
				protected Resource getResource(String resourcePath, Resource location) throws IOException {
					Resource resource = location.createRelative(resourcePath);
					if (resource.exists() && resource.isReadable()) {
						return resource;
					}
					return resourcePath.startsWith("api/") ? null : location.createRelative("index.html");
				}
			});
	}
}
