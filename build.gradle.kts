plugins {
	java
	id("org.springframework.boot") version "4.1.2"
	id("io.spring.dependency-management") version "1.1.7"
}

group = "its.me"
version = "0.0.1-SNAPSHOT"

java {
	toolchain {
		languageVersion = JavaLanguageVersion.of(21)
	}
}

repositories {
	mavenCentral()
}

val fabric8Version = "8.0.0"
val pebbleVersion = "4.1.2"

dependencies {
	implementation("org.springframework.boot:spring-boot-starter-actuator")
	implementation("org.springframework.boot:spring-boot-starter-webmvc")
	implementation("org.springframework.boot:spring-boot-starter-validation")
	implementation("org.springdoc:springdoc-openapi-starter-webmvc-ui:3.2.0")
	implementation("io.fabric8:kubernetes-client:$fabric8Version")
	implementation("io.pebbletemplates:pebble:$pebbleVersion")
	compileOnly("org.projectlombok:lombok")
	annotationProcessor("org.projectlombok:lombok")
	testImplementation("org.springframework.boot:spring-boot-starter-actuator-test")
	testImplementation("org.springframework.boot:spring-boot-starter-webmvc-test")
	testImplementation("io.fabric8:kubernetes-server-mock:$fabric8Version")
	testCompileOnly("org.projectlombok:lombok")
	testRuntimeOnly("org.junit.platform:junit-platform-launcher")
	testAnnotationProcessor("org.projectlombok:lombok")
}

tasks.withType<Test> {
	useJUnitPlatform()
}

// Die API-Beschreibung hat genau eine Quelle: backend/internal/api/openapi.yaml (gemeinsam mit dem Go-Backend).
tasks.processResources {
	from("backend/internal/api") {
		include("openapi.yaml")
		into("api")
	}
}

// Lokal mit der Beispiel-Konfiguration starten.
tasks.named<org.springframework.boot.gradle.tasks.run.BootRun>("bootRun") {
	args("--spring.config.additional-location=file:config-example/application.yml")
}
