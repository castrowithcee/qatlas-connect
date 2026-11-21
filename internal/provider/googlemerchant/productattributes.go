package googlemerchant

// The members of ProductAttributes and of the types it uses, as the product discovery document of the Merchant
// API v1 defines them: 145 members, none deprecated. A member the document does not list is never held.

var nLoyaltyPoints = obj(
	f("name", tName),
	f("pointsValue", tInt),
	f("ratio", tNum),
)

var nPrice = obj(
	f("amountMicros", tInt),
	f("currencyCode", tName),
)

var nShipping = obj(
	f("country", tName),
	f("handlingCutoffTime", tName),
	f("handlingCutoffTimezone", tName),
	f("locationGroupName", tName),
	f("locationId", tInt),
	f("loyaltyProgramLabel", tName),
	f("loyaltyTierLabel", tName),
	f("maxHandlingTime", tInt),
	f("maxTransitTime", tInt),
	f("minHandlingTime", tInt),
	f("minTransitTime", tInt),
	f("postalCode", tName),
	f("price", nPrice),
	f("region", tName),
	f("service", tName),
)

var nShippingBusinessDaysConfig = obj(
	f("businessDays", tName),
	f("country", tName),
)

var nUnitPricingMeasure = obj(
	f("unit", tName),
	f("value", tNum),
)

var nMileage = obj(
	f("unit", tID),
	f("value", tInt),
)

var nProductInstallment = obj(
	f("amount", nPrice),
	f("annualPercentageRate", tNum),
	f("creditType", tID),
	f("downpayment", nPrice),
	f("mileageAllowance", nMileage),
	f("months", tInt),
	f("totalAmount", nPrice),
)

var nQuestionAndAnswer = obj(
	f("answer", tLong),
	f("question", tName),
)

var nReturns = obj(
	f("countries", list(tName)),
	f("itemConditions", list(tID)),
	f("methods", list(tID)),
	f("outcomes", list(tID)),
	f("policyUrl", tURL),
	f("restockingFee", nPrice),
	f("restockingPercentageFee", tNum),
	f("shippingFee", nPrice),
	f("shippingFeeType", tID),
	f("windowDays", tInt),
	f("windowType", tID),
)

var nFuelConsumption = obj(
	f("unit", tID),
	f("value", tNum),
)

var nProductMinimumOrderValue = obj(
	f("country", tName),
	f("price", nPrice),
	f("service", tName),
	f("surface", tID),
)

var nPetPolicy = obj(
	f("petTypes", list(tID)),
	f("petsAllowed", tBool),
)

var nProductDimension = obj(
	f("unit", tName),
	f("value", tNum),
)

var nCo2Emissions = obj(
	f("unit", tID),
	f("value", tInt),
)

var nSubscriptionCost = obj(
	f("amount", nPrice),
	f("period", tID),
	f("periodLength", tInt),
)

var nProductSustainabilityIncentive = obj(
	f("amount", nPrice),
	f("percentage", tNum),
	f("type", tID),
)

var nCloudExportAdditionalProperties = obj(
	f("boolValue", tBool),
	f("floatValue", list(tNum)),
	f("intValue", list(tInt)),
	f("maxValue", tNum),
	f("minValue", tNum),
	f("propertyName", tName),
	f("textValue", list(tName)),
	f("unitCode", tName),
)

var nProductCertification = obj(
	f("certificationAuthority", tID),
	f("certificationCode", tName),
	f("certificationDocumentLink", tURL),
	f("certificationLabelLink", tURL),
	f("certificationName", tID),
	f("certificationValue", tName),
)

var nShippingDimension = obj(
	f("unit", tName),
	f("value", tNum),
)

var nVariantOption = obj(
	f("name", tName),
	f("value", tName),
)

var nProductDetail = obj(
	f("attributeName", tName),
	f("attributeValue", tName),
	f("sectionName", tName),
)

var nEnergyConsumption = obj(
	f("unit", tID),
	f("value", tNum),
)

var nLeaseTerm = obj(
	f("durationUnit", tID),
	f("durationValue", tInt),
	f("type", tID),
)

var nWarranty = obj(
	f("duration", tInt),
	f("durationUnit", tID),
	f("mileage", nMileage),
)

var nStructuredTitle = obj(
	f("content", tLong),
	f("digitalSourceType", tID),
)

var nUnitPricingBaseMeasure = obj(
	f("unit", tName),
	f("value", tInt),
)

var nInterval = obj(
	f("endTime", tID),
	f("startTime", tID),
)

var nLoyaltyProgram = obj(
	f("cashbackForFutureUse", nPrice),
	f("loyaltyPoints", tInt),
	f("memberPriceEffectiveDate", nInterval),
	f("price", nPrice),
	f("programLabel", tName),
	f("shippingLabel", tName),
	f("tierLabel", tName),
)

var nUnitArea = obj(
	f("unit", tID),
	f("value", tNum),
)

var nDisplayAddress = obj(
	f("city", tName),
	f("postalCode", tName),
	f("region", tName),
	f("streetName", tName),
	f("streetNumber", tName),
)

var nFreeShippingThreshold = obj(
	f("country", tName),
	f("priceThreshold", nPrice),
)

var nShippingWeight = obj(
	f("unit", tName),
	f("value", tNum),
)

var nRelatedProduct = obj(
	f("id", tName),
	f("idType", tID),
	f("relationshipType", tID),
)

var nProductWeight = obj(
	f("unit", tName),
	f("value", tNum),
)

var nCarrierShipping = obj(
	f("carrierPrice", tID),
	f("carrierPriceFlatAdjustment", nPrice),
	f("carrierPricePercentageAdjustment", tNum),
	f("carrierTransitTime", tID),
	f("country", tName),
	f("fixedMaxTransitTime", tInt),
	f("fixedMinTransitTime", tInt),
	f("flatPrice", nPrice),
	f("maxHandlingTime", tInt),
	f("minHandlingTime", tInt),
	f("originPostalCode", tName),
	f("postalCode", tName),
	f("region", tName),
)

var nHandlingCutoffTime = obj(
	f("country", tName),
	f("cutoffTime", tName),
	f("cutoffTimezone", tName),
	f("disableDeliveryAfterCutoff", tBool),
)

var nStructuredDescription = obj(
	f("content", tLong),
	f("digitalSourceType", tID),
)

var nProductFee = obj(
	f("amount", nPrice),
	f("type", tID),
)

var nPickupCost = obj(
	f("flatRate", nPrice),
	f("freeThreshold", nPrice),
)

var nProductAttributes = obj(
	f("additionalImageLinks", list(tURL)),
	f("adsGrouping", tName),
	f("adsLabels", list(tName)),
	f("adsRedirect", tURL),
	f("adult", tBool),
	f("ageGroup", tID),
	f("amenityFeature", list(tID)),
	f("autoPricingMinPrice", nPrice),
	f("availability", tID),
	f("availabilityDate", tID),
	f("bodyStyle", tID),
	f("brand", tName),
	f("canonicalLink", tURL),
	f("carrierShipping", list(nCarrierShipping)),
	f("certifications", list(nProductCertification)),
	f("certifiedPreOwned", tBool),
	f("cloudExportAdditionalProperties", list(nCloudExportAdditionalProperties)),
	f("co2Emissions", nCo2Emissions),
	f("color", tName),
	f("condition", tID),
	f("costOfGoodsSold", nPrice),
	f("customLabel0", tName),
	f("customLabel1", tName),
	f("customLabel2", tName),
	f("customLabel3", tName),
	f("customLabel4", tName),
	f("dateFirstRegistered", tName),
	f("description", tLong),
	f("disclosureDate", tID),
	f("displayAddress", nDisplayAddress),
	f("displayAdsId", tName),
	f("displayAdsLink", tURL),
	f("displayAdsSimilarIds", list(tName)),
	f("displayAdsTitle", tName),
	f("displayAdsValue", tNum),
	f("documentLinks", list(tURL)),
	f("electricRange", nMileage),
	f("emissionsStandard", tID),
	f("energyConsumption", nEnergyConsumption),
	f("energyEfficiencyClass", tID),
	f("engine", tID),
	f("excludedDestinations", list(tID)),
	f("expirationDate", tID),
	f("externalSellerId", tName),
	f("freeShippingThreshold", list(nFreeShippingThreshold)),
	f("fuelConsumption", nFuelConsumption),
	f("fuelConsumptionDischargedBattery", nFuelConsumption),
	f("gender", tID),
	f("googleProductCategory", tName),
	f("gtins", list(tName)),
	f("handlingCutoffTimes", list(nHandlingCutoffTime)),
	f("identifierExists", tBool),
	f("imageLink", tURL),
	f("includedDestinations", list(tID)),
	f("installment", nProductInstallment),
	f("isBundle", tBool),
	f("itemGroupId", tName),
	f("itemGroupTitle", tName),
	f("latitude", tNum),
	f("leaseTerm", nLeaseTerm),
	f("lifestyleImageLinks", list(tURL)),
	f("link", tURL),
	f("linkTemplate", tURL),
	f("longitude", tNum),
	f("loyaltyPoints", nLoyaltyPoints),
	f("loyaltyPrograms", list(nLoyaltyProgram)),
	f("material", tName),
	f("maxEnergyEfficiencyClass", tID),
	f("maxHandlingTime", tInt),
	f("maximumRetailPrice", nPrice),
	f("mileage", nMileage),
	f("minEnergyEfficiencyClass", tID),
	f("minHandlingTime", tInt),
	f("minimumOrderValues", list(nProductMinimumOrderValue)),
	f("mobileLink", tURL),
	f("mobileLinkTemplate", tURL),
	f("model", tName),
	f("mpn", tName),
	f("multipack", tInt),
	f("neighborhood", tName),
	f("numberOfBathrooms", tNum),
	f("numberOfBedrooms", tNum),
	f("numberOfUnits", tInt),
	f("pattern", tName),
	f("pause", tID),
	f("petPolicy", nPetPolicy),
	f("pickupCost", nPickupCost),
	f("pickupMethod", tID),
	f("pickupSla", tID),
	f("popularityRank", tNum),
	f("price", nPrice),
	f("productDetails", list(nProductDetail)),
	f("productFee", list(nProductFee)),
	f("productHeight", nProductDimension),
	f("productHighlights", list(tName)),
	f("productLength", nProductDimension),
	f("productTypes", list(tName)),
	f("productWeight", nProductWeight),
	f("productWidth", nProductDimension),
	f("promotionIds", list(tName)),
	f("propertyName", tName),
	f("propertyType", tID),
	f("questionsAndAnswers", list(nQuestionAndAnswer)),
	f("relatedProducts", list(nRelatedProduct)),
	f("returnPolicyLabel", tName),
	f("returns", list(nReturns)),
	f("salePrice", nPrice),
	f("salePriceEffectiveDate", nInterval),
	f("sellOnGoogleQuantity", tInt),
	f("shipping", list(nShipping)),
	f("shippingHandlingBusinessDays", list(nShippingBusinessDaysConfig)),
	f("shippingHeight", nShippingDimension),
	f("shippingLabel", tName),
	f("shippingLength", nShippingDimension),
	f("shippingTransitBusinessDays", list(nShippingBusinessDaysConfig)),
	f("shippingWeight", nShippingWeight),
	f("shippingWidth", nShippingDimension),
	f("shoppingAdsExcludedCountries", list(tName)),
	f("shortTitle", tName),
	f("size", tName),
	f("sizeSystem", tID),
	f("sizeTypes", list(tID)),
	f("specialtyHousingType", tID),
	f("structuredDescription", nStructuredDescription),
	f("structuredTitle", nStructuredTitle),
	f("subscriptionCost", nSubscriptionCost),
	f("sustainabilityIncentives", list(nProductSustainabilityIncentive)),
	f("title", tName),
	f("transitTimeLabel", tName),
	f("trim", tName),
	f("unitArea", nUnitArea),
	f("unitPricingBaseMeasure", nUnitPricingBaseMeasure),
	f("unitPricingMeasure", nUnitPricingMeasure),
	f("utilitiesIncluded", list(tID)),
	f("variantOptions", list(nVariantOption)),
	f("vehicleAllInPrice", nPrice),
	f("vehicleExpenses", nPrice),
	f("vehicleMandatoryInspectionIncluded", tBool),
	f("vehicleMsrp", nPrice),
	f("vehiclePriceType", tID),
	f("videoLinks", list(tURL)),
	f("vin", tName),
	f("virtualModelLink", tURL),
	f("warranty", nWarranty),
	f("year", tInt),
)
