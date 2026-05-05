import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { EmptyState } from "../components/EmptyState";
import { Button } from "../components/Button";

export default function NotFound() {
  const { t } = useTranslation();
  return (
    <EmptyState
      icon="warning"
      title={t("not_found.title")}
      description={t("not_found.description")}
      action={
        <Link to="/medicines">
          <Button variant="primary">{t("not_found.go_home")}</Button>
        </Link>
      }
    />
  );
}
