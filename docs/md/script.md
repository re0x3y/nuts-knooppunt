## Script (from Project Plan PoC 10)

### Demo: Happy Flows

1. Per vendor: show empty/not-yet-synced Query Directories via FHIR server
    * n=1, Dirk shows in LRZa test service.

2. An employee of a healthcare institution logs in to the mock LRZa website and registers the URL of their Organisation Directory there.
    * A vendor of an Organisation Directory adds their customer as an Organisation. a. Per vendor, add extra organisation to Admin Directory. b.

3. An employee of the healthcare institution adds Endpoints, Locations and Healthcare services. 
    * Per vendor, add extra Location and/or extra HealthcareService to Admin
Directory.

4. A vendor of a searchable address book sets the LRZa as the authoritative source for organisations. 
    * n=1, configuration in 'Knooppunt'.

5. The searchable address book synchronises the data from the LRZa and the
organisation. 
    * Per vendor, kick off synchronisation.

6. We can demonstrate this via a query on this searchable address book. 
    * Per vendor, query Query Directory, show synced Query Directory via FHIR server

7. Implement a change in the organisation address book. 
    * Per vendor, implement change in Admin Directory from role of healthcare organisation employee.

8. After a synchronisation, this change is visible in the searchable address book. 
    * Per vendor, kick off synchronisation, query Query Directory, show synced Query Directory via FHIR server.

---

### Demo: Unhappy Flows

1. What if there is data in an admin directory that does not belong to the own organisation? 
    * Diagnosis: organisation admin directory ≠ authoritative source.
    * behaviour: do not adopt → warning

2. HealthcareService, Location, Practitioner Role without reference to an organisation.
    * Desired behaviour: do not adopt → warning

3. URA-identifier of organisation is invalid. 
    * Desired behaviour: do not adopt → warning

4. partOf(parent organisation) of organisation is invalid.
    * Desired behaviour: do not adopt → warning.    

---